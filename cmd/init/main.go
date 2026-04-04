package main

import (
	"fmt"
	"translator/internal/handlers"
	"translator/internal/tts"

	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"
)

func main() {
	fmt.Println("Starting translator of Stuart...")

	// Load envionrment veriables
	err := godotenv.Load(".env")
	
	if err != nil {
		log.Error(err)
	}

	var tts_engine = tts.InitTts()
	tts_engine.Speak("The quick brown fox jumped over the lazy dog", 10)

	// Initialize prompt listening
	var enable_tts = true
	err = handlers.InitLLM(tts_engine, enable_tts)

	if err != nil {
		log.Error(err)
	} else {
		fmt.Println("Closing translator. Goodbye!")
	}
}
