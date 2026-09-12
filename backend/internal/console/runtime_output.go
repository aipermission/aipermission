package console

type RuntimeOutputKind uint8

const (
	RuntimeStdout RuntimeOutputKind = iota
	RuntimeStderr
)

type RuntimeOutput struct {
	Kind RuntimeOutputKind
	Data string
}
