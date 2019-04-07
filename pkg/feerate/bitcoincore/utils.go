package bitcoincore

import (
	"sort"
)

/**
 * Returns index of the first element in the array which is not less than given value.
 * @param {Iterator<number>} iterable
 * @param {number} value
 * @returns {number} index
 */
func lowerBound(iterable []int, value int) int {
	d := make([]int, len(iterable))
	copy(d, iterable)
	sort.Slice(d, func(i, j int) bool {
		crit := Abs(value-d[i]) - Abs(value-d[j])
		return crit < 0
	})

	closestGreaterOrEqualElement := d[0]
	index := Index(iterable, closestGreaterOrEqualElement)
	return index
}

func Index(vs []int, t int) int {
	for i, v := range vs {
		if v == t {
			return i
		}
	}
	return -1
}

func Abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
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

func MakeSlice2D(sizeOneD int, sizeTwoD int) [][]int {
	twoD := make([][]int, sizeOneD)
	for i := 0; i < sizeOneD; i++ {
		twoD[i] = make([]int, sizeTwoD)
	}

	return twoD
}
