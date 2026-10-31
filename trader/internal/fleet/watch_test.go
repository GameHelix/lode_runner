package fleet

import "testing"

func TestPricesReadAsTheExchangeQuotesThem(t *testing.T) {
	for x, want := range map[float64]string{1990.0000000000002: "1990", 103456.5: "103456.5", 2161.08: "2161.08", 0.0083451234: "0.00834512", 0.5: "0.5"} {
		if got := trim(x, priceDecimals(x)); got != want {
			t.Errorf("%v: %s, want %s", x, got, want)
		}
	}
}
