// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package number

import "math"

//go:generate stringer -type=Kind

// Kind describes the data type of the Number.
type Kind int8

const (
	// Int64Kind indicates int64.
	Int64Kind Kind = iota

	// Float64Kind indicates float64.
	Float64Kind
)

// Number is a 64bit numeric value, one of the Any interface types.
type Number uint64

// Any is any of the supported generic Number types.
type Any interface {
	int64 | float64
}

// CoerceToFloat64 converts Number to float64 according to Kind.
func (n Number) CoerceToFloat64(k Kind) float64 {
	if k == Int64Kind {
		return float64(n)
	}
	return math.Float64frombits(uint64(n))
}

// ToFloat64 converts Number to float64.
func ToFloat64(n Number) float64 {
	return math.Float64frombits(uint64(n))
}

// ToInt64 converts Number to int64.
func ToInt64(n Number) int64 {
	return int64(n)
}

// FromFloat64 converts float64 to Number.
func FromFloat64(n float64) Number {
	return Number(math.Float64bits(n))
}

// FromInt64 converts int64 to Number.
func FromInt64(n int64) Number {
	return Number(n)
}
