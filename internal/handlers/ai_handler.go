package handlers

import (
	"bufio"
	"context"
	"fmt"
	"google.golang.org/genai"
	"translator/internal/tts"
	"log"
	"os"
)

func InitLLM(engine *tts.SpeechService, enable_tts bool) error {
	api_key := os.Getenv("GEMINI_API_KEY")
	fmt.Println(api_key)
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
		engine.Speak(result.Text(), 0)
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
		"gemini-3-flash-preview",
		genai.Text(prompt),
		nil,
	)

	return result, err
}
