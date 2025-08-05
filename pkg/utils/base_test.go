package utils

import (
	"fmt"
	"testing"
)

func TestParseLabels(t *testing.T) {
	labels := "xx:yy,xx1:yy1,xx2:yy2"
	values := ParseLabels(labels)
	fmt.Printf("values are %v\n", values)
}
