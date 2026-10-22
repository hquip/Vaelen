package core

// Fish exports ordinary lists separated by spaces. A nested fish imports those
// lists as single strings, but an explicitly marked path variable uses colons.
// Reapply the flag after import: VSM_PATH_BASE's name does not end in PATH, so
// fish cannot infer its list type. Keep directory spaces intact.
func fishPathBase() string {
	return `if not set -q VSM_PATH_BASE
    set -gx --path VSM_PATH_BASE $PATH
else
    set -gx --path VSM_PATH_BASE $VSM_PATH_BASE
end
`
}
