// Copyright 2017 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package remote

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/prompb"
	config_util "github.com/prometheus/prometheus/util/config"
)

var longErrMessage = strings.Repeat("error message", maxErrMsgLen)

func TestStoreHTTPErrorHandling(t *testing.T) {
	tests := []struct {
		code int
		err  error
	}{
		{
			code: 200,
			err:  nil,
		},
		{
			code: 300,
			err:  fmt.Errorf("server returned HTTP status 300 Multiple Choices: " + longErrMessage[:maxErrMsgLen]),
		},
		{
			code: 404,
			err:  fmt.Errorf("server returned HTTP status 404 Not Found: " + longErrMessage[:maxErrMsgLen]),
		},
		{
			code: 500,
			err:  recoverableError{fmt.Errorf("server returned HTTP status 500 Internal Server Error: " + longErrMessage[:maxErrMsgLen])},
		},
	}

	for i, test := range tests {
		server := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, longErrMessage, test.code)
			}),
		)

		serverURL, err := url.Parse(server.URL)
		if err != nil {
			t.Fatal(err)
		}

		c, err := NewClient(0, &ClientConfig{
			URL:     &config_util.URL{URL: serverURL},
			Timeout: model.Duration(time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}

		err = c.Store(&prompb.WriteRequest{})
		if !reflect.DeepEqual(err, test.err) {
			t.Errorf("%d. Unexpected error; want %v, got %v", i, test.err, err)
		}

		server.Close()
	}
}

func TestReadLabelValues(t *testing.T) {
	metaStore := map[string][]string{
		"a": []string{"b", "c", "d"},
	}
	tests := []struct {
		enable         bool
		name           string
		code           int
		expectedValues []string
		err            error
	}{
		{
			enable:         true,
			code:           200,
			name:           "a",
			expectedValues: metaStore["a"],
		},
		{
			enable:         false,
			name:           "a",
			expectedValues: nil,
		},
		{
			enable:         true,
			code:           200,
			name:           "not-exist",
			expectedValues: nil,
		},
		{
			enable: true,
			code:   400,
			name:   "foo",
			err:    fmt.Errorf("server returned HTTP status 400 Bad Request"),
		},
	}

	for i, test := range tests {
		server := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if test.err != nil {
					http.Error(w, longErrMessage, test.code)
					return
				}
				req, err := DecodeLabelValuesRequest(r)
				if err != nil {
					t.Fatal(err)
				}
				resp := &prompb.LabelValuesResponse{
					LabelValues: metaStore[req.LabelName],
				}
				EncodeLabelValuesResponse(resp, w)
			}),
		)
		defer server.Close()

		serverURL, err := url.Parse(server.URL)
		if err != nil {
			t.Fatal(err)
		}

		c, err := NewClient(0, &ClientConfig{
			LabelValuesURL: &config_util.URL{URL: serverURL},
			Timeout:        model.Duration(time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
		if !test.enable {
			c.labelValuesURL = nil
		}

		labelValues, err := c.LabelValues(context.Background(), test.name)
		if !reflect.DeepEqual(err, test.err) {
			t.Errorf("%d. Unexpected error; want %v, got %v", i, test.err, err)
		}

		if !reflect.DeepEqual(labelValues, test.expectedValues) {
			t.Errorf("%d. Expect label values %v, got %v", i, test.expectedValues, labelValues)
		}

	}
}
