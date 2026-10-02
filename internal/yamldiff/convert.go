// Copyright © 2018 The Homeport Team
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.
//
// Ported from github.com/gonvenience/ytbx v1.5.0 (input.go, convert.go)
// and modified for graft: JSON and TOML values become yamlnode trees
// through yamlnode.FromValue, and JSON keeps ytbx's default of decoding
// numbers as float64 with keys in sorted order.

package yamldiff

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

// loadJSONDocuments reads data as a stream of JSON values and returns one
// document per value.
func loadJSONDocuments(data []byte) ([]*yamlnode.Node, error) {
	values := []*yamlnode.Node{}

	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var value interface{}
		err := decoder.Decode(&value)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}

		node, err := yamlnode.FromValue(value)
		if err != nil {
			return nil, err
		}

		values = append(values, node)
	}

	return values, nil
}

// loadTOMLDocuments reads data as TOML, which has exactly one document.
func loadTOMLDocuments(data []byte) ([]*yamlnode.Node, error) {
	var value interface{}
	if err := toml.Unmarshal(data, &value); err != nil {
		return nil, err
	}

	_, offset := time.Now().In(time.Local).Zone()

	node, err := yamlnode.FromValue(rezoneLocalTimes(value, offset))
	if err != nil {
		return nil, err
	}

	return []*yamlnode.Node{node}, nil
}

// rezoneLocalTimes returns value with every TOML local date, local
// datetime, and local time moved into the zone whose offset from UTC is
// offset seconds, keeping the same wall-clock fields. BurntSushi/toml
// builds those zones once, when its package initializes, so they follow
// the process zone at startup rather than time.Local when the file is
// read. Redoing the zone here makes a load follow time.Local, which only
// tests ever reassign. Other times, including RFC 3339 values with their
// own offsets, pass through unchanged.
func rezoneLocalTimes(value interface{}, offset int) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		for key, item := range v {
			v[key] = rezoneLocalTimes(item, offset)
		}
		return v
	case []interface{}:
		for i, item := range v {
			v[i] = rezoneLocalTimes(item, offset)
		}
		return v
	case []map[string]interface{}:
		for _, item := range v {
			rezoneLocalTimes(item, offset) // Rewrites the map in place.
		}
		return v
	case time.Time:
		return rezoneLocalTime(v, offset)
	default:
		return value
	}
}

// rezoneLocalTime moves t into a zone with the same name and the given
// offset when t is one of the TOML local zones, and returns t otherwise.
func rezoneLocalTime(t time.Time, offset int) time.Time {
	name := t.Location().String()
	switch name {
	case "datetime-local", "date-local", "time-local":
		year, month, day := t.Date()
		hour, minute, second := t.Clock()
		return time.Date(year, month, day, hour, minute, second, t.Nanosecond(), time.FixedZone(name, offset))
	default:
		return t
	}
}
