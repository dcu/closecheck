package main

import "net/http"

func main() {
	defer http.Get("https://www.google.com")
}
