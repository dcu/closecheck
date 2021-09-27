package main

import "net/http"

type Closable struct {
	resp *http.Response
}

func New() (*Closable, error) {
	resp, err := http.Get("https://www.google.com")
	if err != nil {
		return nil, err
	}

	return &Closable{
		resp: resp,
	}, nil
}

func (c *Closable) Close() error {
	return c.resp.Body.Close()
}

func main() {
	c, err := New()
	if err != nil {
		panic(err)
	}

	c.Close()
}
