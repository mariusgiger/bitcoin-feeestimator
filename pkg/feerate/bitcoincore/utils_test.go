package bitcoincore

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShouldFindClosestElementInArrayWhenValueNotInArray(t *testing.T) {
	// arrange
	val := 8
	array := []int{5, 10, 15, 9, 20, 25, 30, 35}

	// act
	boundIndex := lowerBound(array, val)

	// assert
	assert.Equal(t, 3, boundIndex)
}

func TestShouldFindClosestElementInArrayWhenValueInArray(t *testing.T) {
	// arrange
	val := 20
	array := []int{10, 20, 30, 30, 20, 10, 10, 20}

	// act
	boundIndex := lowerBound(array, val)

	// assert
	assert.Equal(t, 1, boundIndex)
}
