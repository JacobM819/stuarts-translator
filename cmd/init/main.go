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

	var tts_engine = tts.StartSpeachEngine()
	tts_engine.Speak("this is a test", 0)

	// Initialize prompt listening
	var enable_tts = true
	err = handlers.InitLLM(tts_engine, enable_tts)

	if err != nil {
		log.Error(err)
	} else {
		fmt.Println("Closing translator. Goodbye!")
	}
}
