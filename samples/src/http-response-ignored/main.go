package main

import "net/http"

func main() {
	_, err := http.Get("https://www.google.com") // want `_.Body \(io.ReadCloser\) was not closed`
	if err != nil {
		panic(err)
	}
}
