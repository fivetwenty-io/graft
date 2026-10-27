package yq

// aliasSpellings lists, for each operator, the spellings that yq's lexer
// accepts for it besides the one graft registers it under. rewrite
// renames a call to its operator, so this table is the one place the
// spellings live and every arity of the operator answers to them.
var aliasSpellings = map[string][]string{
	"map_values":     {"mapvalues"},
	"from_unix":      {"fromunix"},
	"to_unix":        {"tounix"},
	"sort_keys":      {"sortKeys", "sortkeys"},
	"array_to_map":   {"arraytomap", "array_tomap", "arrayto_map"},
	"to_yaml":        {"toyaml"},
	"from_yaml":      {"fromyaml"},
	"load_str":       {"loadstr", "str_load"},
	"split_doc":      {"splitDoc", "splitdoc"},
	"unique_by":      {"uniqueby"},
	"group_by":       {"groupby"},
	"sort_by":        {"sortby"},
	"is_key":         {"iskey"},
	"file_index":     {"fileindex"},
	"document_index": {"documentindex"},
	"setpath":        {"set_path"},
	"delpaths":       {"del_paths"},
	"to_entries":     {"toentries", "toEntries"},
	"from_entries":   {"fromentries", "fromEntries"},
	"with_entries":   {"withentries", "withEntries"},
}

// aliases maps each spelling in aliasSpellings to its operator.
var aliases = func() map[string]string {
	m := map[string]string{}
	for op, spellings := range aliasSpellings {
		for _, s := range spellings {
			m[s] = op
		}
	}
	return m
}()
