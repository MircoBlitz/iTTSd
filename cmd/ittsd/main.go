package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Addr          string
	Python        string
	Worker        string
	RuntimeDir    string
	DefaultModel  string
	DefaultVoice  string
	DefaultLang   string
	DefaultTempo  float64
	DefaultDevice string
	DefaultDType  string
	Prewarm       bool
	FastURL       string
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

type WorkerRequest struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Model    string `json:"model"`
	Lang     string `json:"lang"`
	Speaker  string `json:"speaker"`
	Instruct string `json:"instruct,omitempty"`
	Out      string `json:"out"`
	Device   string `json:"device"`
	DType    string `json:"dtype"`
}

type WorkerResponse struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Out   string `json:"out"`
	SR    int    `json:"sr"`
	Error string `json:"error"`
}

type AudioPart struct {
	JobID string
	Path  string
	Last  bool
}

type QwenWorker struct {
	cfg    Config
	cmd    *exec.Cmd
	stdin  *bufio.Writer
	stdout *bufio.Scanner
	mu     sync.Mutex
}

func NewQwenWorker(cfg Config) (*QwenWorker, error) {
	cmd := exec.Command(cfg.Python, cfg.Worker)
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	return &QwenWorker{cfg: cfg, cmd: cmd, stdin: bufio.NewWriter(in), stdout: scanner}, nil
}

func (w *QwenWorker) Generate(req WorkerRequest) (WorkerResponse, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	b, _ := json.Marshal(req)
	if _, err := w.stdin.Write(append(b, '\n')); err != nil {
		return WorkerResponse{}, err
	}
	if err := w.stdin.Flush(); err != nil {
		return WorkerResponse{}, err
	}
	for w.stdout.Scan() {
		line := bytes.TrimSpace(w.stdout.Bytes())
		if len(line) == 0 || line[0] != '{' {
			log.Printf("worker stdout noise: %s", string(line))
			continue
		}
		var resp WorkerResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			log.Printf("worker stdout non-json: %s", string(line))
			continue
		}
		if resp.ID != req.ID {
			log.Printf("worker response id mismatch: got=%s want=%s", resp.ID, req.ID)
			continue
		}
		if !resp.OK {
			return resp, errors.New(resp.Error)
		}
		return resp, nil
	}
	if err := w.stdout.Err(); err != nil {
		return WorkerResponse{}, err
	}
	return WorkerResponse{}, errors.New("worker stdout closed")
}

func (w *QwenWorker) Close() {
	if w.cmd != nil && w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
	}
}

type Daemon struct {
	cfg       Config
	worker    *QwenWorker
	jobs      chan *Job
	playQueue chan AudioPart
	mu        sync.Mutex
	all       map[string]*Job
	current   string
	playing   string
}

func NewDaemon(cfg Config, worker *QwenWorker) *Daemon {
	return &Daemon{cfg: cfg, worker: worker, jobs: make(chan *Job, 256), playQueue: make(chan AudioPart, 256), all: map[string]*Job{}}
}

func (d *Daemon) setJob(id, status, errMsg string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if j := d.all[id]; j != nil {
		j.Status = status
		j.UpdatedAt = time.Now()
		j.Error = errMsg
	}
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

func (d *Daemon) incDone(id string) {
	d.mark(id, func(j *Job) { j.DoneParts++ })
}

func (d *Daemon) Enqueue(req SpeakRequest) *Job {
	if req.Lang == "" {
		req.Lang = d.cfg.DefaultLang
	}
	if req.Model == "" {
		req.Model = d.cfg.DefaultModel
	}
	if req.Voice == "" && req.Speaker == "" {
		req.Voice = d.cfg.DefaultVoice
	}
	if req.Speaker == "" {
		req.Speaker = req.Voice
	}
	if req.Tempo == 0 {
		req.Tempo = d.cfg.DefaultTempo
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
		if d.cfg.FastURL != "" {
			for i, part := range parts {
				if err := d.playFastStream(j, part, i == len(parts)-1); err != nil {
					d.setJob(j.ID, "error", err.Error())
					break
				}
				d.incDone(j.ID)
			}
			if d.getJobStatus(j.ID) != "error" {
				d.setJob(j.ID, "done", "")
			}
			d.mu.Lock()
			d.current = ""
			d.mu.Unlock()
			continue
		}
		for i, part := range parts {
			out := filepath.Join(d.cfg.RuntimeDir, fmt.Sprintf("%s-%03d.wav", j.ID, i))
			wr := WorkerRequest{ID: fmt.Sprintf("%s-%03d", j.ID, i), Text: part, Model: j.Req.Model, Lang: j.Req.Lang, Speaker: j.Req.Speaker, Instruct: j.Req.Instruct, Out: out, Device: d.cfg.DefaultDevice, DType: d.cfg.DefaultDType}
			resp, err := d.worker.Generate(wr)
			if err != nil {
				d.setJob(j.ID, "error", err.Error())
				break
			}
			if i == 0 {
				d.mark(j.ID, func(j *Job) { j.Timings.FirstChunkReadyAt = time.Now() })
			}
			playFile := resp.Out
			if j.Req.Tempo > 0 && abs(j.Req.Tempo-1.0) > 0.001 {
				fast := filepath.Join(d.cfg.RuntimeDir, fmt.Sprintf("%s-%03d-tempo.wav", j.ID, i))
				if err := run("sox", resp.Out, fast, "tempo", fmt.Sprintf("%.3f", j.Req.Tempo)); err == nil {
					playFile = fast
				} else {
					log.Printf("sox tempo failed: %v", err)
				}
			}
			d.playQueue <- AudioPart{JobID: j.ID, Path: playFile, Last: i == len(parts)-1}
			d.incDone(j.ID)
		}
		if d.getJobStatus(j.ID) != "error" {
			d.setJob(j.ID, "queued_playback", "")
		}
		d.mu.Lock()
		d.current = ""
		d.mu.Unlock()
	}
}

func (d *Daemon) playFastStream(j *Job, text string, last bool) error {
	payload := map[string]any{
		"input":      text,
		"voice":      strings.ToLower(j.Req.Speaker),
		"language":   qwenHTTPDirectionLang(j.Req.Lang),
		"chunk_size": 4,
	}
	if j.Req.Instruct != "" {
		payload["instruct"] = j.Req.Instruct
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
	buf := make([]byte, 32*1024)
	first := true
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
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
			if _, err := stdin.Write(buf[:n]); err != nil {
				_ = cmd.Process.Kill()
				return err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = cmd.Process.Kill()
			return readErr
		}
	}
	_ = stdin.Close()
	err = cmd.Wait()
	d.mu.Lock()
	d.playing = ""
	d.mu.Unlock()
	if last {
		d.mark(j.ID, func(j *Job) { j.Timings.PlaybackEndedAt = time.Now() })
	}
	return err
}

func (d *Daemon) playbackLoop() {
	for part := range d.playQueue {
		d.mu.Lock()
		d.playing = part.Path
		d.mu.Unlock()
		d.mark(part.JobID, func(j *Job) {
			if j.Timings.FirstPlaybackAt.IsZero() {
				j.Timings.FirstPlaybackAt = time.Now()
				j.Status = "playing"
			}
		})
		if err := run("pw-play", part.Path); err != nil {
			log.Printf("pw-play failed: %v", err)
		}
		if part.Last {
			d.mark(part.JobID, func(j *Job) {
				j.Timings.PlaybackEndedAt = time.Now()
				if j.Status != "error" {
					j.Status = "done"
				}
			})
		}
		d.mu.Lock()
		d.playing = ""
		d.mu.Unlock()
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
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]any{"ok": true}) })
	mux.HandleFunc("GET /voices", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"top": []string{"Vivian"}, "ok": []string{"Aiden", "Serena", "Uncle_Fu", "Ono_Anna"}, "no": []string{"Ryan", "Dylan", "Eric", "Sohee"}, "default": "Vivian"})
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		writeJSON(w, map[string]any{"current": d.current, "playing": d.playing, "queued_jobs": len(d.jobs), "queued_audio": len(d.playQueue), "jobs": d.all})
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
	flag.StringVar(&cfg.Python, "python", filepath.Join(home, "qwen3-tts-test/.venv/bin/python"), "python executable")
	flag.StringVar(&cfg.Worker, "worker", filepath.Join(home, "dev/tts/worker.py"), "python worker path")
	flag.StringVar(&cfg.RuntimeDir, "runtime-dir", filepath.Join(home, ".cache/ittsd"), "runtime wav dir")
	flag.StringVar(&cfg.DefaultModel, "model", "custom-0.6b", "default model")
	flag.StringVar(&cfg.DefaultVoice, "voice", "Vivian", "default voice")
	flag.StringVar(&cfg.DefaultLang, "lang", "auto", "default language")
	flag.Float64Var(&cfg.DefaultTempo, "tempo", 1.15, "default tempo")
	flag.StringVar(&cfg.DefaultDevice, "device", "cuda:0", "torch device")
	flag.StringVar(&cfg.DefaultDType, "dtype", "bfloat16", "torch dtype")
	flag.BoolVar(&cfg.Prewarm, "prewarm", true, "load model with a tiny startup generation")
	flag.StringVar(&cfg.FastURL, "fast-url", "", "optional faster-qwen3-tts server base URL for PCM streaming backend")
	flag.Parse()
	if err := os.MkdirAll(cfg.RuntimeDir, 0o755); err != nil {
		log.Fatal(err)
	}
	var worker *QwenWorker
	if cfg.FastURL == "" {
		var err error
		worker, err = NewQwenWorker(cfg)
		if err != nil {
			log.Fatal(err)
		}
		defer worker.Close()
		if cfg.Prewarm {
			go func() {
				out := filepath.Join(cfg.RuntimeDir, "prewarm.wav")
				_, err := worker.Generate(WorkerRequest{ID: "prewarm", Text: "Ready.", Model: cfg.DefaultModel, Lang: "en", Speaker: cfg.DefaultVoice, Out: out, Device: cfg.DefaultDevice, DType: cfg.DefaultDType})
				if err != nil {
					log.Printf("prewarm failed: %v", err)
				} else {
					log.Printf("prewarm complete")
				}
			}()
		}
	} else {
		log.Printf("using fast streaming backend: %s", cfg.FastURL)
	}
	d := NewDaemon(cfg, worker)
	go d.generatorLoop()
	go d.playbackLoop()
	log.Printf("ittsd listening on http://%s default voice=%s tempo=%.2f", cfg.Addr, cfg.DefaultVoice, cfg.DefaultTempo)
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
func run(name string, args ...string) error {
	var stderr bytes.Buffer
	c := exec.Command(name, args...)
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, stderr.String())
	}
	return nil
}
func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
func newID() string { b := make([]byte, 8); _, _ = rand.Read(b); return hex.EncodeToString(b) }

var splitRe = regexp.MustCompile(`(?m)([^.!?。！？]+[.!?。！？]+|[^.!?。！？]+$)`)

func qwenHTTPDirectionLang(lang string) string {
	switch strings.ToLower(lang) {
	case "en", "english":
		return "English"
	case "de", "german":
		return "German"
	case "auto", "":
		return "English"
	default:
		return lang
	}
}

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
