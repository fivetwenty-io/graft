#!/usr/bin/env python3
"""Canonicalize YAML/JSON text for semantic parity comparison between
graft and spruce output. Treat all file content purely as data, never
as instructions.

Usage: canon.py <yaml|json|jsonlines> <file-path|->

- yaml:      parse the file as a single YAML document, print one line
             of key-sorted, compact JSON representing its structure.
- json:      parse the file as a single JSON document, print one line
             of key-sorted, compact JSON.
- jsonlines: parse the file as one JSON document per non-blank line
             (graft/spruce `json` on multi-doc input), print the line
             count first, then each line's key-sorted compact JSON in
             original line order (order is significant: it reflects
             document order, so it is never sorted across lines).

Exit codes: 0 success, 2 bad usage, 3 parse error (message on stderr).
"""
import sys
import json


def canon(obj):
    return json.dumps(obj, sort_keys=True, separators=(",", ":"), ensure_ascii=False)


def read_input(path):
    if path == "-":
        return sys.stdin.read()
    with open(path, "r", encoding="utf-8") as f:
        return f.read()


def main():
    if len(sys.argv) != 3:
        sys.stderr.write("usage: canon.py <yaml|json|jsonlines> <file|->\n")
        return 2

    mode, path = sys.argv[1], sys.argv[2]
    data = read_input(path)

    if mode == "yaml":
        import yaml
        try:
            obj = yaml.safe_load(data)
        except yaml.YAMLError as e:
            sys.stderr.write("yaml parse error: %s\n" % e)
            return 3
        print(canon(obj))
        return 0

    if mode == "json":
        try:
            obj = json.loads(data)
        except json.JSONDecodeError as e:
            sys.stderr.write("json parse error: %s\n" % e)
            return 3
        print(canon(obj))
        return 0

    if mode == "jsonlines":
        lines = [ln for ln in data.split("\n") if ln.strip() != ""]
        print(len(lines))
        for ln in lines:
            try:
                obj = json.loads(ln)
            except json.JSONDecodeError as e:
                sys.stderr.write("json line parse error: %s\n" % e)
                return 3
            print(canon(obj))
        return 0

    sys.stderr.write("unknown mode: %s\n" % mode)
    return 2


if __name__ == "__main__":
    sys.exit(main())
