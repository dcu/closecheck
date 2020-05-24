package main

import "net/http"

func main() {
	_, err := http.Get("https://www.google.com")
	if err != nil {
		panic(err)
	}
}
