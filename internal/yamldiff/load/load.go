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
// Ported from github.com/gonvenience/ytbx v1.5.0 (input.go) and modified
// for graft: documents are yamlnode trees parsed with goccy, load and
// parse failures are an Error that can style its location, and the
// larger functions are split into helpers that keep every decision in
// ytbx's order.

// Package load reads diff inputs from stdin, files, directories, and
// URIs. It lives outside internal/yamldiff so the diff core reads only
// the bytes it is given.
package load

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fivetwenty-io/graft/internal/yamldiff"
	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

// Error is a failure to read or parse an input location. Op is "load"
// or "parse".
type Error struct {
	Op       string
	Location string
	Kind     yamldiff.LocationKind
	Err      error
}

// Error returns the message with the location unstyled. Like ytbx's
// HumanReadableLocation with color off, it names stdin as "stdin".
func (e *Error) Error() string { return e.Styled(nil) }

// Unwrap returns the underlying load or parse error.
func (e *Error) Unwrap() error { return e.Err }

// Styled returns the message with the location passed through style, the
// way ytbx's HumanReadableLocation colors it. A nil style leaves it plain.
func (e *Error) Styled(style yamldiff.LocationStyler) string {
	loc := e.Location
	switch {
	case style != nil:
		loc = style(e.Kind, e.Location)
	case e.Kind == yamldiff.LocationStdin:
		loc = "stdin"
	}
	return fmt.Sprintf("unable to %s data from %s: %s", e.Op, loc, e.Err)
}

// ClassifyLocation makes the decision ytbx's HumanReadableLocation makes.
// Stdin comes first, then an existing path, then anything that parses as a
// request URI, which includes absolute paths that do not exist.
func ClassifyLocation(location string) yamldiff.LocationKind {
	if IsStdin(location) {
		return yamldiff.LocationStdin
	}
	if _, err := os.Stat(location); err == nil {
		return yamldiff.LocationFile
	}
	if _, err := url.ParseRequestURI(location); err == nil {
		return yamldiff.LocationURI
	}
	return yamldiff.LocationPlain
}

// Files loads two locations concurrently. When both fail it reports
// the from-location's error, as ytbx does.
func Files(fromLocation, toLocation string) (yamldiff.InputFile, yamldiff.InputFile, error) {
	type resultPair struct {
		result yamldiff.InputFile
		err    error
	}

	fromChan := make(chan resultPair, 1)
	toChan := make(chan resultPair, 1)

	go func() {
		result, err := File(fromLocation)
		fromChan <- resultPair{result, err}
	}()

	go func() {
		result, err := File(toLocation)
		toChan <- resultPair{result, err}
	}()

	from := <-fromChan
	if from.err != nil {
		return yamldiff.InputFile{}, yamldiff.InputFile{}, from.err
	}

	to := <-toChan
	if to.err != nil {
		return yamldiff.InputFile{}, yamldiff.InputFile{}, to.err
	}

	return from.result, to.result, nil
}

// File loads one location as YAML, JSON, or TOML. A directory goes to
// Directory, and its error comes back unwrapped.
func File(location string) (yamldiff.InputFile, error) {
	if info, err := os.Stat(location); err == nil && info.IsDir() {
		return Directory(location)
	}

	data, err := getBytesFromLocation(location)
	if err != nil {
		return yamldiff.InputFile{}, &Error{Op: "load", Location: location, Kind: ClassifyLocation(location), Err: err}
	}

	documents, err := Documents(data)
	if err != nil {
		return yamldiff.InputFile{}, &Error{Op: "parse", Location: location, Kind: ClassifyLocation(location), Err: err}
	}

	return yamldiff.InputFile{Location: location, Documents: documents}, nil
}

// Directory loads every entry of a directory, in name order, as
// documents. Names holds one name per file.
func Directory(location string) (yamldiff.InputFile, error) {
	files, err := os.ReadDir(location)
	if err != nil {
		return yamldiff.InputFile{}, fmt.Errorf("failed to read files in directory %s: %w", location, err)
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Name() < files[j].Name()
	})

	result := yamldiff.InputFile{Location: location}

	for _, file := range files {
		path := filepath.Join(location, file.Name())

		data, err := getBytesFromLocation(path)
		if err != nil {
			return yamldiff.InputFile{}, err
		}

		docs, err := Documents(data)
		if err != nil {
			return yamldiff.InputFile{}, fmt.Errorf("failed to read %s: %w", path, err)
		}

		result.Documents = append(result.Documents, docs...)
		result.Names = append(result.Names, file.Name())
	}

	return result, nil
}

// Documents reads data as TOML, JSON, or YAML, with any number of
// documents. Empty input is one null document. TOML is tried first, then
// yamldiff.ParseDocuments reads JSON or YAML.
func Documents(data []byte) ([]*yamlnode.Node, error) {
	// Empty input is a null document, and TOML would read it as an empty
	// table instead.
	if yamldiff.IsEmpty(data) {
		return yamldiff.ParseDocuments(data)
	}

	// There is no cheap check for TOML, so trying to parse it is the test.
	if docs, err := loadTOMLDocuments(data); err == nil {
		return docs, nil
	}

	return yamldiff.ParseDocuments(data)
}

// getBytesFromLocation reads stdin, a file, or a URI, in that order.
func getBytesFromLocation(location string) ([]byte, error) {
	if IsStdin(location) {
		return io.ReadAll(os.Stdin)
	}

	if _, err := os.Stat(location); err == nil {
		// #nosec G304 -- the path is a diff argument the user chose, which the CLI is meant to read
		return os.ReadFile(location)
	}

	if _, err := url.ParseRequestURI(location); err == nil {
		return fetchURI(location)
	}

	return nil, fmt.Errorf("unable to get any content using location %s: it is not a file or usable URI", location)
}

// fetchURI reads the body of a GET request. A status other than 200 fails
// with the body as the reason.
func fetchURI(location string) ([]byte, error) {
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, location, http.NoBody)
	if err != nil {
		return nil, err
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	data, err := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to retrieve data from location %s: %s", location, string(data))
	}

	return data, err
}

// IsStdin reports whether location is the dash that stands for standard
// input, ignoring surrounding space.
func IsStdin(location string) bool {
	return strings.TrimSpace(location) == "-"
}
