package version

import (
	"fmt"
	"github.com/chirino/svcteleporter/internal/cmd"
	"github.com/spf13/cobra"
)

func New() *cobra.Command {
	return &cobra.Command{
		Use: `version`,
		Run: func(c *cobra.Command, args []string) {
			fmt.Println(cmd.Version)
		},
	}
}
