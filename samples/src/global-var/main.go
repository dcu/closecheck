package main

import (
	"io"
	"os"
)

var globalCloser io.Closer

func init() {
	globalCloser, _ = os.Open("")
}

func main() {
}
