package semantic

// layoutProfile is the grammar-specific policy consumed by the otherwise
// grammar-neutral syntax matcher and normalizer. Adding a language should be a
// data change here plus one parser binding, not a new rendering algorithm.
type layoutProfile struct {
	delimiters          map[string]layoutDelimiter
	owners              map[string]layoutOwnerRule
	initializerPatterns map[string]initializerPattern
	moduleLiteralRoles  map[string]bool
}

type layoutDelimiter struct {
	open  string
	close string
}

type layoutOwnerKind uint8

const (
	ownerBinding layoutOwnerKind = iota
	ownerECMADeclaration
	ownerModule
	ownerECMAExport
	ownerSingleton
	ownerIdentity
)

type layoutOwnerRule struct {
	kind              layoutOwnerKind
	bindingContainers map[string]bool
	allowUnkeyed      bool
}

type initializerPattern struct {
	patterns  map[string]bool
	operators map[string]bool
}

func languageProfiles() map[string]layoutProfile {
	ecmaDelimiters := delimiters(
		"object", "{", "}", "object_pattern", "{", "}", "named_imports", "{", "}",
		"export_clause", "{", "}", "array", "[", "]", "array_pattern", "[", "]",
	)
	ecmaOwners := map[string]layoutOwnerRule{
		"import_statement":         {kind: ownerModule, allowUnkeyed: true},
		"export_statement":         {kind: ownerECMAExport},
		"lexical_declaration":      {kind: ownerECMADeclaration},
		"variable_declaration":     {kind: ownerBinding, bindingContainers: roles("variable_declarator")},
		"jsx_element":              {kind: ownerIdentity},
		"jsx_self_closing_element": {kind: ownerIdentity},
	}
	ecmaInitializer := map[string]initializerPattern{
		"variable_declarator": {patterns: roles("object_pattern", "array_pattern"), operators: roles("=")},
	}
	moduleLiterals := roles("string", "interpreted_string_literal", "raw_string_literal", "string_literal", "system_lib_string")

	return map[string]layoutProfile{
		"typescript": {
			delimiters: ecmaDelimiters, owners: ecmaOwners,
			initializerPatterns: ecmaInitializer, moduleLiteralRoles: moduleLiterals,
		},
		"javascript": {
			delimiters: ecmaDelimiters, owners: ecmaOwners,
			initializerPatterns: ecmaInitializer, moduleLiteralRoles: moduleLiterals,
		},
		"go": {
			delimiters: delimiters(
				"literal_value", "{", "}", "argument_list", "(", ")", "parameter_list", "(", ")",
				"type_arguments", "[", "]",
			),
			owners: map[string]layoutOwnerRule{
				"import_declaration":    {kind: ownerModule, allowUnkeyed: true},
				"var_declaration":       {kind: ownerBinding, bindingContainers: roles("var_spec")},
				"const_declaration":     {kind: ownerBinding, bindingContainers: roles("const_spec")},
				"short_var_declaration": {kind: ownerBinding},
				"assignment_statement":  {kind: ownerBinding},
			},
			moduleLiteralRoles: moduleLiterals,
		},
		"python": {
			delimiters: delimiters(
				"dictionary", "{", "}", "set", "{", "}", "list", "[", "]", "list_pattern", "[", "]",
				"argument_list", "(", ")", "parameters", "(", ")", "lambda_parameters", "(", ")",
			),
			owners: map[string]layoutOwnerRule{
				"import_statement":        {kind: ownerModule, allowUnkeyed: true},
				"import_from_statement":   {kind: ownerModule, allowUnkeyed: true},
				"future_import_statement": {kind: ownerModule, allowUnkeyed: true},
				"assignment":              {kind: ownerBinding},
			},
			moduleLiteralRoles: moduleLiterals,
		},
		"rust": {
			delimiters: delimiters(
				"field_initializer_list", "{", "}", "field_declaration_list", "{", "}", "use_list", "{", "}",
				"array_expression", "[", "]", "arguments", "(", ")", "parameters", "(", ")",
				"tuple_expression", "(", ")", "tuple_pattern", "(", ")", "struct_pattern", "{", "}",
			),
			owners: map[string]layoutOwnerRule{
				"use_declaration": {kind: ownerModule, allowUnkeyed: true},
				"let_declaration": {kind: ownerBinding},
				"const_item":      {kind: ownerBinding},
				"static_item":     {kind: ownerBinding},
			},
			moduleLiteralRoles: moduleLiterals,
		},
		"java": {
			delimiters: delimiters(
				"array_initializer", "{", "}", "element_value_array_initializer", "{", "}",
				"argument_list", "(", ")", "formal_parameters", "(", ")", "annotation_argument_list", "(", ")",
				"type_arguments", "<", ">",
			),
			owners: map[string]layoutOwnerRule{
				"import_declaration":         {kind: ownerModule, allowUnkeyed: true},
				"local_variable_declaration": {kind: ownerBinding, bindingContainers: roles("variable_declarator")},
				"field_declaration":          {kind: ownerBinding, bindingContainers: roles("variable_declarator")},
				"constant_declaration":       {kind: ownerBinding, bindingContainers: roles("variable_declarator")},
			},
			moduleLiteralRoles: moduleLiterals,
		},
		"json": {
			delimiters: delimiters("object", "{", "}", "array", "[", "]"),
			owners:     map[string]layoutOwnerRule{"document": {kind: ownerSingleton}},
		},
		"c": {
			delimiters: delimiters(
				"initializer_list", "{", "}", "argument_list", "(", ")", "parameter_list", "(", ")",
				"enumerator_list", "{", "}",
			),
			owners: map[string]layoutOwnerRule{
				"preproc_include": {kind: ownerModule, allowUnkeyed: true},
				"declaration":     {kind: ownerBinding, bindingContainers: roles("init_declarator")},
			},
			moduleLiteralRoles: moduleLiterals,
		},
		"cpp": {
			delimiters: delimiters(
				"initializer_list", "{", "}", "argument_list", "(", ")", "parameter_list", "(", ")",
				"enumerator_list", "{", "}", "template_argument_list", "<", ">", "template_parameter_list", "<", ">",
			),
			owners: map[string]layoutOwnerRule{
				"preproc_include": {kind: ownerModule, allowUnkeyed: true},
				"declaration":     {kind: ownerBinding, bindingContainers: roles("init_declarator", "structured_binding_declarator")},
			},
			moduleLiteralRoles: moduleLiterals,
		},
		"ruby": {
			delimiters: delimiters("array", "[", "]", "array_pattern", "[", "]"),
			owners: map[string]layoutOwnerRule{
				"assignment":          {kind: ownerBinding},
				"operator_assignment": {kind: ownerBinding},
			},
		},
	}
}

func delimiters(values ...string) map[string]layoutDelimiter {
	result := make(map[string]layoutDelimiter, len(values)/3)
	for index := 0; index+2 < len(values); index += 3 {
		result[values[index]] = layoutDelimiter{open: values[index+1], close: values[index+2]}
	}
	return result
}

func roles(values ...string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}
