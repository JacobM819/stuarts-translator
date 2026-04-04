package tts

import (
	"encoding/binary"
	"io"
	"log"
	"math"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	oto "github.com/ebitengine/oto/v3"
	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
	flag "github.com/spf13/pflag"
)

type SpeechService struct {
	Engine     *sherpa.OfflineTts
	otoContext *oto.Context
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

func InitTts() *SpeechService {
	config := sherpa.OfflineTtsConfig{}
	config.Model.Kokoro.Model = "./internal/tts/assets/model.onnx"
	config.Model.Kokoro.Voices = "./internal/tts/assets/voices.bin"
	config.Model.Kokoro.Tokens = "./internal/tts/assets/tokens.txt"
	config.Model.Kokoro. DataDir = "./internal/tts/assets/espeak-ng-data"
	config.Model.NumThreads = 4

	sid := 0

	flag.Parse()

	log.Println("Speaker ID:", sid)

	log.Println("Initializing model (may take several seconds)")
	tts := sherpa.NewOfflineTts(&config)

	if tts == nil {
		log.Fatal("Failed to create TTS engine. Check if config filepaths are correct.")
	}


	log.Println("Model created!")

	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   tts.SampleRate(),
		ChannelCount: 1,
		Format:       oto.FormatSignedInt16LE,
	})

	<-ready

	if err != nil {
		log.Fatal(err)
	}
	ss := &SpeechService{
		Engine: tts,
		otoContext: ctx,
	}
	return ss
}

func (ss *SpeechService) Speak(text string, voice int) {

	pcmBuf := newPCMBuffer()

	reader := &pcmReader{
		buf:  pcmBuf,
		done: make(chan struct{}),
	}

	player := ss.otoContext.NewPlayer(reader)
	player.Play()
	defer player.Close()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	// var generated *sherpa.GeneratedAudio

	start := time.Now()
	cfg := sherpa.GenerationConfig{
		SilenceScale: 0.2,
		Speed:        1.0,
		Sid:          voice,
	}

	go func() {
		defer pcmBuf.Finish()

		ss.Engine.GenerateWithConfig(
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
	// For saving audio to file
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
