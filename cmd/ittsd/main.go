package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

const Version = "v0.2.0"

var supportedPresetVoices = []string{"serena", "vivian", "aiden", "dylan", "eric", "ryan", "ono_anna", "sohee", "uncle_fu"}

var openAIAliases = map[string]string{
	"alloy":   "ryan",
	"echo":    "aiden",
	"fable":   "dylan",
	"onyx":    "eric",
	"nova":    "serena",
	"shimmer": "vivian",
}

type Config struct {
	Addr         string
	RuntimeDir   string
	DefaultVoice string
	DefaultLang  string
	FastURL      string
}

type SpeakRequest struct {
	Text     string  `json:"text"`
	Lang     string  `json:"lang,omitempty"`
	Voice    string  `json:"voice,omitempty"`
	Speaker  string  `json:"speaker,omitempty"`
	Model    string  `json:"model,omitempty"`
	Tempo    float64 `json:"tempo,omitempty"`
	Instruct string  `json:"instruct,omitempty"`
	Priority int     `json:"priority,omitempty"`
}

type Timings struct {
	AcceptedAt          time.Time `json:"accepted_at"`
	GenerationStartedAt time.Time `json:"generation_started_at,omitempty"`
	FirstChunkReadyAt   time.Time `json:"first_chunk_ready_at,omitempty"`
	FirstPlaybackAt     time.Time `json:"first_playback_at,omitempty"`
	PlaybackEndedAt     time.Time `json:"playback_ended_at,omitempty"`
}

type Latencies struct {
	AcceptedToGenerationMS    int64 `json:"accepted_to_generation_ms,omitempty"`
	AcceptedToFirstChunkMS    int64 `json:"accepted_to_first_chunk_ms,omitempty"`
	AcceptedToFirstPlaybackMS int64 `json:"accepted_to_first_playback_ms,omitempty"`
	FirstChunkToPlaybackMS    int64 `json:"first_chunk_to_playback_ms,omitempty"`
	TotalPlaybackMS           int64 `json:"total_playback_ms,omitempty"`
}

type Job struct {
	ID        string       `json:"id"`
	Req       SpeakRequest `json:"request"`
	Status    string       `json:"status"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
	Error     string       `json:"error,omitempty"`
	Parts     int          `json:"parts"`
	DoneParts int          `json:"done_parts"`
	Timings   Timings      `json:"timings"`
	Latencies Latencies    `json:"latencies"`
}

type Daemon struct {
	cfg     Config
	jobs    chan *Job
	mu      sync.Mutex
	all     map[string]*Job
	current string
	playing string
}

func NewDaemon(cfg Config) *Daemon {
	return &Daemon{cfg: cfg, jobs: make(chan *Job, 256), all: map[string]*Job{}}
}

func (d *Daemon) setJob(id, status, errMsg string) {
	d.mark(id, func(j *Job) { j.Status, j.Error = status, errMsg })
}

func (d *Daemon) mark(id string, fn func(*Job)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if j := d.all[id]; j != nil {
		fn(j)
		j.UpdatedAt = time.Now()
		computeLatencies(j)
	}
}

func (d *Daemon) Enqueue(req SpeakRequest) *Job {
	if req.Lang == "" {
		req.Lang = d.cfg.DefaultLang
	}
	if req.Voice == "" && req.Speaker == "" {
		req.Voice = d.cfg.DefaultVoice
	}
	if req.Speaker == "" {
		req.Speaker = req.Voice
	}
	now := time.Now()
	j := &Job{ID: newID(), Req: req, Status: "queued", CreatedAt: now, UpdatedAt: now, Timings: Timings{AcceptedAt: now}}
	d.mu.Lock()
	d.all[j.ID] = j
	d.mu.Unlock()
	d.jobs <- j
	return j
}

func (d *Daemon) generatorLoop() {
	for j := range d.jobs {
		d.mu.Lock()
		d.current = j.ID
		d.mu.Unlock()
		d.setJob(j.ID, "generating", "")
		d.mark(j.ID, func(j *Job) { j.Timings.GenerationStartedAt = time.Now() })
		parts := splitText(j.Req.Text)
		if len(parts) == 0 {
			d.setJob(j.ID, "error", "empty text")
			continue
		}
		d.mark(j.ID, func(j *Job) { j.Parts = len(parts) })
		for i, part := range parts {
			if err := d.playFastStream(j, part, i == len(parts)-1); err != nil {
				d.setJob(j.ID, "error", err.Error())
				break
			}
			d.mark(j.ID, func(j *Job) { j.DoneParts++ })
		}
		if d.getJobStatus(j.ID) != "error" {
			d.setJob(j.ID, "done", "")
		}
		d.mu.Lock()
		d.current = ""
		d.mu.Unlock()
	}
}

func (d *Daemon) playFastStream(j *Job, text string, last bool) error {
	voice := normalizeVoice(j.Req.Speaker)
	if mapped, ok := openAIAliases[voice]; ok {
		voice = mapped
	}
	instruct := j.Req.Instruct
	payload := map[string]any{"input": text, "voice": voice, "language": qwenHTTPDirectionLang(j.Req.Lang), "chunk_size": 4}
	if instruct != "" {
		payload["instruct"] = instruct
	}
	b, _ := json.Marshal(payload)
	url := strings.TrimRight(d.cfg.FastURL, "/") + "/v1/audio/speech/pcm-stream"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("fast backend status %d: %s", resp.StatusCode, string(body))
	}
	cmd := exec.Command("pw-play", "--raw", "--rate", "24000", "--channels", "1", "--format", "s16", "-")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	d.mu.Lock()
	d.playing = "fast-pcm-stream"
	d.mu.Unlock()
	defer func() { d.mu.Lock(); d.playing = ""; d.mu.Unlock() }()
	first := true
	for {
		var hdr [4]byte
		if _, err := io.ReadFull(resp.Body, hdr[:]); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			_ = cmd.Process.Kill()
			return err
		}
		n := binary.BigEndian.Uint32(hdr[:])
		if n == 0 {
			break
		}
		pcm := make([]byte, n)
		if _, err := io.ReadFull(resp.Body, pcm); err != nil {
			_ = cmd.Process.Kill()
			return err
		}
		applyS16Gain(pcm, 0.85)
		if first {
			first = false
			now := time.Now()
			d.mark(j.ID, func(j *Job) {
				if j.Timings.FirstChunkReadyAt.IsZero() {
					j.Timings.FirstChunkReadyAt = now
				}
				if j.Timings.FirstPlaybackAt.IsZero() {
					j.Timings.FirstPlaybackAt = now
					j.Status = "playing"
				}
			})
		}
		if _, err := stdin.Write(pcm); err != nil {
			_ = cmd.Process.Kill()
			return err
		}
	}
	_ = stdin.Close()
	err = cmd.Wait()
	if last {
		d.mark(j.ID, func(j *Job) { j.Timings.PlaybackEndedAt = time.Now() })
	}
	return err
}

func applyS16Gain(pcm []byte, gain float64) {
	if gain == 1 || len(pcm) < 2 {
		return
	}
	for i := 0; i+1 < len(pcm); i += 2 {
		v := int16(binary.LittleEndian.Uint16(pcm[i:]))
		scaled := int(float64(v) * gain)
		if scaled > 32767 {
			scaled = 32767
		} else if scaled < -32768 {
			scaled = -32768
		}
		binary.LittleEndian.PutUint16(pcm[i:], uint16(int16(scaled)))
	}
}

func (d *Daemon) getJobStatus(id string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if j := d.all[id]; j != nil {
		return j.Status
	}
	return ""
}

func (d *Daemon) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "version": Version, "backend": "qwen3-fast-tts", "model": "Qwen3-TTS-12Hz-0.6B-CustomVoice"})
	})
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"version": Version})
	})
	mux.HandleFunc("GET /voices", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"model":                 "Qwen3-TTS-12Hz-0.6B-CustomVoice",
			"default":               strings.ToLower(d.cfg.DefaultVoice),
			"preset":                supportedPresetVoices,
			"aliases":               openAIAliases,
			"custom_clones_enabled": false,
		})
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		writeJSON(w, map[string]any{"current": d.current, "playing": d.playing, "queued_jobs": len(d.jobs), "jobs": d.all})
	})
	mux.HandleFunc("POST /speak", func(w http.ResponseWriter, r *http.Request) {
		var req SpeakRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if strings.TrimSpace(req.Text) == "" {
			http.Error(w, "text required", 400)
			return
		}
		if err := validateSpeakVoice(req, d.cfg.DefaultVoice); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		j := d.Enqueue(req)
		writeJSON(w, map[string]any{"job_id": j.ID, "status": j.Status})
	})
	mux.HandleFunc("GET /job/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/job/")
		d.mu.Lock()
		j := d.all[id]
		d.mu.Unlock()
		if j == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, j)
	})
	return mux
}

func main() {
	home, _ := os.UserHomeDir()
	cfg := Config{}
	flag.StringVar(&cfg.Addr, "addr", "127.0.0.1:8765", "listen address")
	flag.StringVar(&cfg.RuntimeDir, "runtime-dir", home+"/.cache/ittsd", "runtime dir")
	flag.StringVar(&cfg.DefaultVoice, "voice", "Vivian", "default voice")
	flag.StringVar(&cfg.DefaultLang, "lang", "auto", "default language")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.StringVar(&cfg.FastURL, "fast-url", "http://127.0.0.1:8001", "faster-qwen3-tts server base URL")
	flag.Parse()
	if *showVersion {
		fmt.Println(Version)
		return
	}
	if cfg.FastURL == "" {
		log.Fatal("--fast-url is required; iTTSd is Go-only and uses an external streaming backend")
	}
	if err := os.MkdirAll(cfg.RuntimeDir, 0o755); err != nil {
		log.Fatal(err)
	}
	d := NewDaemon(cfg)
	go d.generatorLoop()
	log.Printf("ittsd listening on http://%s default voice=%s backend=%s", cfg.Addr, cfg.DefaultVoice, cfg.FastURL)
	server := &http.Server{Addr: cfg.Addr, Handler: d.routes(), ReadHeaderTimeout: 5 * time.Second}
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	_ = server.Shutdown(context.Background())
}

func computeLatencies(j *Job) {
	a := j.Timings.AcceptedAt
	if !a.IsZero() && !j.Timings.GenerationStartedAt.IsZero() {
		j.Latencies.AcceptedToGenerationMS = j.Timings.GenerationStartedAt.Sub(a).Milliseconds()
	}
	if !a.IsZero() && !j.Timings.FirstChunkReadyAt.IsZero() {
		j.Latencies.AcceptedToFirstChunkMS = j.Timings.FirstChunkReadyAt.Sub(a).Milliseconds()
	}
	if !a.IsZero() && !j.Timings.FirstPlaybackAt.IsZero() {
		j.Latencies.AcceptedToFirstPlaybackMS = j.Timings.FirstPlaybackAt.Sub(a).Milliseconds()
	}
	if !j.Timings.FirstChunkReadyAt.IsZero() && !j.Timings.FirstPlaybackAt.IsZero() {
		j.Latencies.FirstChunkToPlaybackMS = j.Timings.FirstPlaybackAt.Sub(j.Timings.FirstChunkReadyAt).Milliseconds()
	}
	if !a.IsZero() && !j.Timings.PlaybackEndedAt.IsZero() {
		j.Latencies.TotalPlaybackMS = j.Timings.PlaybackEndedAt.Sub(a).Milliseconds()
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func newID() string { b := make([]byte, 8); _, _ = rand.Read(b); return hex.EncodeToString(b) }

func normalizeVoice(voice string) string {
	v := strings.ToLower(strings.TrimSpace(voice))
	return strings.ReplaceAll(v, "-", "_")
}

func isSupportedVoice(voice string) bool {
	v := normalizeVoice(voice)
	if _, ok := openAIAliases[v]; ok {
		return true
	}
	for _, candidate := range supportedPresetVoices {
		if v == candidate {
			return true
		}
	}
	return false
}

func validateSpeakVoice(req SpeakRequest, defaultVoice string) error {
	voice := req.Speaker
	if voice == "" {
		voice = req.Voice
	}
	if voice == "" {
		voice = defaultVoice
	}
	if !isSupportedVoice(voice) {
		return fmt.Errorf("unsupported voice %q for 0.6B preset-only mode; use one of: %s", voice, strings.Join(supportedPresetVoices, ", "))
	}
	return nil
}

func qwenHTTPDirectionLang(lang string) string {
	switch strings.ToLower(lang) {
	case "en", "english":
		return "English"
	case "de", "german":
		return "German"
	case "fr", "french", "français", "francais":
		return "French"
	case "zh", "cn", "chinese", "mandarin", "中文", "普通话":
		return "Chinese"
	case "ja", "jp", "japanese":
		return "Japanese"
	case "ko", "kr", "korean":
		return "Korean"
	case "es", "spanish":
		return "Spanish"
	case "it", "italian":
		return "Italian"
	case "pt", "portuguese":
		return "Portuguese"
	case "ru", "russian":
		return "Russian"
	case "auto", "":
		return "English"
	default:
		return lang
	}
}

var splitRe = regexp.MustCompile(`(?m)([^.!?。！？]+[.!?。！？]+|[^.!?。！？]+$)`)

func splitText(s string) []string {
	s = strings.TrimSpace(s)
	matches := splitRe.FindAllString(s, -1)
	var out []string
	for _, m := range matches {
		m = strings.TrimSpace(m)
		if m != "" {
			out = append(out, m)
		}
	}
	return out
}
