package tts

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	oto "github.com/ebitengine/oto/v3"
	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)


func sanitizeForTTS(text string) string {
	text = strings.ReplaceAll(text, "*", "")
	text = strings.ReplaceAll(text, "#", "")
	text = strings.ReplaceAll(text, "_", "")
	text = strings.ReplaceAll(text, "!", ".")
	text = strings.ReplaceAll(text, "?", ".")
	safeChars := regexp.MustCompile(`[^a-zA-Z0-9\s.,'":;-]`)
	text = safeChars.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "..", ".")
	text = strings.TrimSpace(text)
	text = strings.Join(strings.Fields(text), " ")
	text = strings.ReplaceAll(text, "\n", " ")
	fmt.Println(text)

	return text
}


type pcmBuffer struct {
	mu       sync.Mutex
	queue    [][]byte
	finished bool
	started  chan struct{} // closed on first callback
	once     sync.Once
}

func newPCMBuffer() *pcmBuffer {
	return &pcmBuffer{
		started: make(chan struct{}),
	}
}

func (b *pcmBuffer) Push(p []byte) {
	b.once.Do(func() {
		close(b.started)
	})

	b.mu.Lock()
	b.queue = append(b.queue, p)
	b.mu.Unlock()
}

func (b *pcmBuffer) Finish() {
	b.once.Do(func() {
		close(b.started)
	})

	b.mu.Lock()
	b.finished = true
	b.mu.Unlock()
}

type pcmReader struct {
	buf  *pcmBuffer
	done chan struct{}
	once sync.Once
}

func (r *pcmReader) Read(p []byte) (int, error) {
	<-r.buf.started

	r.buf.mu.Lock()
	defer r.buf.mu.Unlock()

	// 2) Have audio
	if len(r.buf.queue) > 0 {
		chunk := r.buf.queue[0]
		n := copy(p, chunk)

		if n == len(chunk) {
			r.buf.queue = r.buf.queue[1:]
		} else {
			r.buf.queue[0] = chunk[n:]
		}
		return n, nil
	}

	// 3) Finished → EOF
	if r.buf.finished {
		r.once.Do(func() { close(r.done) })
		return 0, io.EOF
	}

	// 4) Gap → silence
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// SpeechService holds the AI model and the speaker context
type SpeechService struct {
	Engine     *sherpa.OfflineTts
	otoContext *oto.Context
}

// NewSpeechService initializes the model ONCE.
func StartSpeachEngine() *SpeechService {
	config := sherpa.OfflineTtsConfig{}
	// Update these paths to where you saved the models in your project
	config.Model.Kokoro.Model = "./internal/tts/assets/model.onnx"
	config.Model.Kokoro.Voices = "./internal/tts/assets/voices.bin"
	config.Model.Kokoro.Tokens = "./internal/tts/assets/tokens.txt"
	config.Model.Kokoro.DataDir = "./internal/tts/assets/espeak-ng-data"
	config.Model.NumThreads = 4

	var err error

	engine := sherpa.NewOfflineTts(&config)
	if engine == nil {
		err = errors.New("Could not initialize TTS engine. Check file paths.")
		log.Fatal(err)
	}

	// Initialize the speaker context
	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   engine.SampleRate(),
		ChannelCount: 1,
		Format:       oto.FormatSignedInt16LE,
	})
	if err != nil {
		err = errors.New("Failed to init audio context:")
		log.Fatal("Failed to init audio context:", err)
	}
	<-ready // Wait for audio hardware to be ready

	return &SpeechService{
		Engine:     engine,
		otoContext: ctx,
	}
}

// Speak generates the audio and plays it immediately.
func (s *SpeechService) Speak(text string, speakerID int) {
	log.Println("AI Generating speech...")
	cfg := sherpa.GenerationConfig{
		Speed: 1.0,
		Sid:   speakerID,
	}

	// 2. Setup the Buffer and Reader
	pcmBuf := newPCMBuffer()
	reader := &pcmReader{
		buf:  pcmBuf,
		done: make(chan struct{}),
	}

	// 3. Start Playing
	player := s.otoContext.NewPlayer(reader)
	player.Play()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	// Variable for saving audio files
	// var generated *sherpa.GeneratedAudio

	start := time.Now()
	text = sanitizeForTTS(text)

	go func() {
		defer pcmBuf.Finish()

		// generated = 
		s.Engine.GenerateWithConfig(
			text,
			&cfg,
			func(samples []float32, progress float32) bool {
				log.Printf("Progress: %.1f%%", progress*100)

				buf := make([]byte, len(samples)*2)
				for i, s := range samples {
					if s > 1 {
						s = 1
					} else if s < -1 {
						s = -1
					}
					v := int16(math.Round(float64(s * 32767)))
					binary.LittleEndian.PutUint16(buf[i*2:], uint16(v))
				}

				pcmBuf.Push(buf)
				return true
			},
		)

		log.Println("TTS generation finished in", time.Since(start))
	}()

	select {
	case <-stop:
		log.Println("Interrupted")
	case <-reader.done:
		log.Println("Playback finished")
	}

	// If you want to save audio to a file
	/*if generated != nil {
		if ok := generated.Save(filename); !ok {
			log.Println("Failed to save audio")
		} else {
			log.Println("Saved generated audio to", filename)
		}
	}*/

	// let remaining audio drain
	time.Sleep(800 * time.Millisecond)

	log.Println("Done")
}
