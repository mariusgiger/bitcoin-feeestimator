package core

import "sort"

func lowerBound(m map[float64]int, val float64) int {
	var keys []float64
	for k := range m {
		keys = append(keys, k)
	}
	sort.Float64s(keys)
	bound := 0
	for _, k := range keys {
		if k >= val {
			bound = m[k]
		}
	}

	return bound
}

func Min(x, y int) int {
	if x < y {
		return x
	}
	return y
}

func Max(x, y int) int {
	if x > y {
		return x
	}
	return y
}

func MinU(x, y uint) uint {
	if x < y {
		return x
	}
	return y
}

func MaxU(x, y uint) uint {
	if x > y {
		return x
	}
	return y
}
