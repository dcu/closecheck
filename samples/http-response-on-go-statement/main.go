package main

import "net/http"

func main() {
	go http.Get("https://www.google.com")
}
