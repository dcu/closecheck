package main

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
)

func main() {
	res, err := http.Get("https://www.google.com")
	if err != nil {
		panic(err)
	}

	//defer res.Body.Close()

	nopCloser := ioutil.NopCloser(res.Body)

	v := map[string]interface{}{}
	_ = json.NewDecoder(nopCloser).Decode(&v)
}
