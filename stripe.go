package lib

import (
	"fmt"
	"log"

	"github.com/stripe/stripe-go"
	"github.com/stripe/stripe-go/charge"
)

func StripeCharge(amount int64, desc string) string {
	// Set Stripe API key
	stripe.Key = "YOUR_API_KEY"
	sourceId := "tok_visa"

	// Create a new charge
	params := &stripe.ChargeParams{
		Amount:      stripe.Int64(amount),
		Currency:    stripe.String(string(stripe.CurrencyUSD)),
		Description: stripe.String(desc),
	}
	params.SetSource(sourceId) // use a test card token provided by Stripe
	ch, err := charge.New(params)

	// Check for errors
	if err != nil {
		log.Fatal(err)
	}

	// Print charge details
	fmt.Printf("Charge ID: %s\n", ch.ID)
	fmt.Printf("Amount: %d\n", ch.Amount)
	fmt.Printf("Description: %s\n", ch.Description)
	return ch.ID
}
