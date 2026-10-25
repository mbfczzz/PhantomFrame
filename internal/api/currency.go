package api

import "math"

const currencyCode = "CNY"

// Unit rates retain six decimal places; wallets and final transaction totals remain fen.
func priceMicros(p Row) int64 {
	if p["unit_price_micros"] != nil {
		return integer(p["unit_price_micros"])
	}
	return integer(p["credits"]) * 10000
}
func priceField(body Row, field string) float64 {
	value := numberField(body, field, 0, 1000000)
	if math.Abs(value*1000000-math.Round(value*1000000)) > 0.0001 {
		fail(400, "单价最多支持六位小数")
	}
	return value
}

// Public amounts remain in yuan (legacy "credits" fields are 1:1).
// The existing integer ledger unit is one fen; no balance conversion is needed.
func moneyField(body Row, field string, min, max float64) float64 {
	value := numberField(body, field, min, max)
	if math.Abs(value*100-math.Round(value*100)) > 1e-6 {
		fail(400, "金额最多支持两位小数（精确到分）")
	}
	return value
}
