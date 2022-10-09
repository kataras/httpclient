package main

import (
	"context"
	"log"
)

const city = "Greece/Athens"

func main() {
	c := NewClient(Options{APIKey: "YOUR_API_KEY_GOES_HERE_GET_ONE_FROM: https://weatherapi.com"})
	resp, err := c.GetCurrentByCity(context.Background(), city)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Temp (C): %f\n", resp.Current.TempC)
}
