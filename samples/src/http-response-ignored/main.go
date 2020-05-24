package main

import "net/http"

func main() {
	_, err := http.Get("https://www.google.com") // want "net/http.Response.Body should be closed"
	if err != nil {
		panic(err)
	}
}
