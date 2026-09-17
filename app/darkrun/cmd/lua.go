package cmd

import (
	"fmt"
	"os"

	"github.com/darkspinnet/darkspin/content/lua51"
	"github.com/spf13/cobra"
)

func newLuaCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "lua <bytecode>",
		Short: "Decode and disassemble a native Lua 5.1 chunk",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			contents, err := os.ReadFile(arguments[0])
			if err != nil {
				return fmt.Errorf("luaRead: %w", err)
			}
			chunk, err := lua51.Inspect(contents)
			if err != nil {
				return fmt.Errorf("luaDecode: %w", err)
			}
			err = lua51.WriteDisassembly(command.OutOrStdout(), chunk)
			if err != nil {
				return fmt.Errorf("luaDisassemble: %w", err)
			}
			return nil
		},
	}
}
