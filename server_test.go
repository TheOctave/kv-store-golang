package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestJSON(t *testing.T) {

	header := http.Header{}
	headerKey := "Content-Type"
	headerValue := "application/json; charset=utf-8"
	header.Add(headerKey, headerValue)

	testcases := []struct {
		in     any
		header http.Header
		out    string
	}{
		{map[string]string{"hello": "world"}, header, `{"hello":"world"}`},
		{map[string]string{"hello": "tables"}, header, `{"hello":"tables"}`},
		{make(chan bool), header, `{"error":"json: unsupported type: chan bool"}`},
	}

	for _, testcase := range testcases {
		recorder := httptest.NewRecorder()

		JSON(recorder, testcase.in)
		response := recorder.Result()
		defer response.Body.Close()

		got, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatalf("Error reading response body: %s", err)
		}

		if string(got) != testcase.out {
			t.Errorf("Got %s, expected %s", string(got), testcase.out)
		}

		if contentType := response.Header.Get(headerKey); contentType != headerValue {
			t.Errorf("Got %s, expected %s", contentType, headerValue)
		}
	}

}

func TestGet(t *testing.T) {
	makeStorage(t)
	defer cleanupStorage(t)

	kvStore := map[string]string{
		"key1": "value1",
		"key2": "value2",
		"key3": "value3",
	}
	encodedStore := map[string]string{}
	for key, value := range kvStore {
		encodedKey := base64.URLEncoding.EncodeToString([]byte(key))
		encodedValue := base64.URLEncoding.EncodeToString([]byte(value))
		encodedStore[encodedKey] = encodedValue
	}

	fileContents, _ := json.Marshal(encodedStore)
	os.WriteFile(storagePath+"/data.json", fileContents, 0644)

	testCases := []struct {
		in  string
		out string
		err error
	}{
		{"key1", "value1", nil},
		{"key2", "value2", nil},
		{"key3", "value3", nil},
	}

	for _, test := range testCases {
		got, err := Get(context.Background(), test.in)
		if err != nil {
			t.Errorf("Received unexpected error: %s", err)
		}
		if got != test.out {
			t.Errorf("Got %s, expected %s", got, test.out)
		}
	}
}

func makeStorage(t *testing.T) {
	err := os.Mkdir("testdata", 0755)
	if err != nil && !os.IsExist(err) {
		t.Fatalf("Couldn't create directory testdata: %s", err)
	}

	storagePath = "testdata"
}

func cleanupStorage(t *testing.T) {
	if err := os.RemoveAll(storagePath); err != nil {
		t.Errorf("Failed to delete storage path: %s", storagePath)
	}

	storagePath = "/tmp/kv"
}
