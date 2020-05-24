package main

import "net/http"

func main() {
	go http.Get("https://www.google.com") // want "net/http.Response.Body should be closed"
}
