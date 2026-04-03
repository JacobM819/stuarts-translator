package handlers

import (
	"bufio"
	"context"
	"fmt"
	"google.golang.org/genai"
	"log"
	"os"
	"translator/internal/tts"
)

func InitLLM(engine *tts.SpeechService, enable_tts bool) error {
	api_key := os.Getenv("GEMINI_API_KEY")
	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: api_key})
	if err != nil {
		log.Fatal(err)
	}

	var c = make(chan string)

	for {
		go listen_for_prompt(c)

		result, err := prompt_llm(client, ctx, c)

		if err != nil {
			log.Fatal(err)
			break
		} else if result == nil {
			break
		}
		fmt.Println(result.Text())
		engine.Speak(result.Text(), 5)
	}

	return err
}

func listen_for_prompt(c chan string) string {

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("Speak mortal:")
	scanner.Scan()

	var output string = scanner.Text()
	c <- output

	return output
}

func prompt_llm(client *genai.Client, ctx context.Context, c chan string) (*genai.GenerateContentResponse, error) {

	var prompt string = <-c

	if prompt == "stop" {
		return nil, nil
	}

	result, err := client.Models.GenerateContent(
		ctx,
		"gemini-2.5-flash-lite",
		genai.Text(prompt),
		nil,
	)

	return result, err
}
