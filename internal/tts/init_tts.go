package tts

import (
	"encoding/binary"
	"errors"
	"io"
	"log"
	"math"
	"sync"

	oto "github.com/ebitengine/oto/v3"
	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)

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
// Speak generates the audio and plays it immediately.
func (s *SpeechService) Speak(text string, speakerID int) {
	log.Println("AI Generating speech...")
	cfg := sherpa.GenerationConfig{
		Speed: 1.0,
		Sid:   speakerID,
	}

	// 1. Generate the ENTIRE audio block first (No callback needed for Offline models)
	generated := s.Engine.GenerateWithConfig(text, &cfg, nil)

	if generated == nil || len(generated.Samples) == 0 {
		log.Println("Error: AI failed to generate speech.")
		return
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

	// 4. Convert float32 samples to Int16 bytes safely
	buf := make([]byte, len(generated.Samples)*2)
	for i, sample := range generated.Samples {
		// Prevent audio clipping (popping noises)
		if sample > 1 {
			sample = 1
		} else if sample < -1 {
			sample = -1
		}

		v := int16(math.Round(float64(sample * 32767.0)))
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(v))
	}

	// 5. Push the entire audio block and immediately close the buffer
	pcmBuf.Push(buf)
	pcmBuf.Finish()

	// 6. Wait for the audio to finish playing out of the speakers
	<-reader.done

	player.Close()
	log.Println("Finished speaking.")
}
