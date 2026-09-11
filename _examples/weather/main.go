package main

import (
	"context"
	"log"
)

const city = "Greece/Athens"

func main() {
	ctx := context.Background()
	c := NewClient(Options{APIKey: "YOUR_API_KEY_GOES_HERE_GET_ONE_FROM: https://weatherapi.com"})

	resp, err := c.GetCurrentByCity(ctx, city)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Temp (C): %f\n", resp.Current.TempC)

	// The same call through the generic method.
	current, err := c.GetCurrent(ctx, city)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Feels like (C): %f\n", current.Current.FeelslikeC)
}
