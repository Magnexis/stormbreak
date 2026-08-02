package httpretry_test

import (
	"fmt"
	"net/http"

	"github.com/magnexis/stormbreak"
	"github.com/magnexis/stormbreak/httpretry"
)

func ExampleTransport() {
	budget, _ := stormbreak.NewBudget(stormbreak.Config{Capacity: 20})
	client := &http.Client{Transport: &httpretry.Transport{
		Budget: budget,
		Policy: stormbreak.DefaultPolicy(),
	}}

	fmt.Printf("%T\n", client.Transport)
	// Output: *httpretry.Transport
}
