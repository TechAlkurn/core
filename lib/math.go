package lib

import "math"

func ToFixed(num float64, precision int) float64 {
	output := math.Pow(10, float64(precision))
	round := int(num + math.Copysign(0.5, num))
	return float64(round) / output
}

func Round(num float64, precision int) float64 {
	output := math.Pow(10, float64(precision))
	return math.Trunc(num*output) / output
}

func Ceil(num float64, precision int) float64 {
	output := math.Pow(10, float64(precision))
	return math.Ceil(num*output) / output
}

func Floor(num float64, precision int) float64 {
	output := math.Pow(10, float64(precision))
	return math.Floor(num*output) / output
}

func Abs(num float64) float64 {
	return math.Abs(num)
}

func Min(num1 float64, num2 float64) float64 {
	return math.Min(num1, num2)
}

func Max(num1 float64, num2 float64) float64 {
	return math.Max(num1, num2)
}

func Pow(num1 float64, num2 float64) float64 {
	return math.Pow(num1, num2)
}

func Sqrt(num float64) float64 {
	return math.Sqrt(num)
}
