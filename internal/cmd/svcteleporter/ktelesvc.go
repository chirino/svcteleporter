package svcteleporter

import (
	"github.com/chirino/svcteleporter/internal/cmd/create"
	"github.com/chirino/svcteleporter/internal/cmd/exporter"
	"github.com/chirino/svcteleporter/internal/cmd/importer"
	"github.com/chirino/svcteleporter/internal/cmd/version"
	"github.com/spf13/cobra"
)

func New() *cobra.Command {
	var result = &cobra.Command{
		// BashCompletionFunction: bashCompletionFunction,
		Use: `svcteleporter`,
	}
	result.AddCommand(importer.New())
	result.AddCommand(create.New())
	result.AddCommand(exporter.New())
	result.AddCommand(version.New())
	return result
}
