package provider

import (
	"bytes"
	"encoding/json"
	"math/big"
	"strings"
)

// Decode only for comparison, never for serialization. UseNumber avoids
// equating distinct integers/decimals after a float64 round trip.
func decoded(raw json.RawMessage) any {
	if !json.Valid(raw) {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return nil
	}
	return nativeJSONNumbers(value)
}

func nativeJSONNumbers(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for k, item := range v {
			v[k] = nativeJSONNumbers(item)
		}
	case []any:
		for i, item := range v {
			v[i] = nativeJSONNumbers(item)
		}
	case json.Number:
		// Canonical coefficient/exponent equality also accepts 1, 1.0 and 1e0.
		// Keep the exponent symbolic; expanding huge exponents would be unbounded.
		text := strings.ToLower(string(v))
		mantissa, exponent, hasExponent := strings.Cut(text, "e")
		var power big.Int
		if hasExponent {
			power.SetString(exponent, 10)
		}
		sign := ""
		if strings.HasPrefix(mantissa, "-") {
			sign = "-"
			mantissa = mantissa[1:]
		}
		if dot := strings.IndexByte(mantissa, '.'); dot >= 0 {
			power.Sub(&power, big.NewInt(int64(len(mantissa)-dot-1)))
			mantissa = mantissa[:dot] + mantissa[dot+1:]
		}
		mantissa = strings.TrimLeft(mantissa, "0")
		if mantissa == "" {
			return json.Number("0")
		}
		digits := strings.TrimRight(mantissa, "0")
		power.Add(&power, big.NewInt(int64(len(mantissa)-len(digits))))
		if power.Sign() == 0 {
			return json.Number(sign + digits)
		}
		return json.Number(sign + digits + "e" + power.String())
	}
	return value
}
