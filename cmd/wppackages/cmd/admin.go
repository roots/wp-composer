package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/roots/wp-packages/internal/auth"
	"github.com/spf13/cobra"
)

var adminCmd = &cobra.Command{
	Use:   "admin",
	Short: "Admin user management commands",
}

var adminCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an admin user",
	RunE: func(cmd *cobra.Command, args []string) error {
		email, _ := cmd.Flags().GetString("email")
		name, _ := cmd.Flags().GetString("name")

		if email == "" || name == "" {
			return fmt.Errorf("--email and --name are required")
		}

		// Idempotent: if user already exists, log and return
		if _, err := auth.GetUserByEmail(cmd.Context(), application.DB, email); err == nil {
			application.Logger.Info("admin user already exists", "email", email)
			return nil
		}

		password, err := requirePasswordFromStdin(cmd)
		if err != nil {
			return err
		}

		hash, err := auth.HashPassword(password)
		if err != nil {
			return err
		}

		user, err := auth.CreateUser(cmd.Context(), application.DB, email, name, hash, true)
		if err != nil {
			return err
		}

		application.Logger.Info("admin user created", "email", user.Email, "name", user.Name)
		return nil
	},
}

var adminPromoteCmd = &cobra.Command{
	Use:   "promote",
	Short: "Promote an existing user to admin",
	RunE: func(cmd *cobra.Command, args []string) error {
		email, _ := cmd.Flags().GetString("email")
		if email == "" {
			return fmt.Errorf("--email is required")
		}

		if err := auth.PromoteToAdmin(cmd.Context(), application.DB, email); err != nil {
			return err
		}

		application.Logger.Info("user promoted to admin", "email", email)
		return nil
	},
}

var adminResetPasswordCmd = &cobra.Command{
	Use:   "reset-password",
	Short: "Reset a user's password",
	RunE: func(cmd *cobra.Command, args []string) error {
		email, _ := cmd.Flags().GetString("email")
		if email == "" {
			return fmt.Errorf("--email is required")
		}

		password, err := requirePasswordFromStdin(cmd)
		if err != nil {
			return err
		}

		hash, err := auth.HashPassword(password)
		if err != nil {
			return err
		}

		if err := auth.UpdatePassword(cmd.Context(), application.DB, email, hash); err != nil {
			return err
		}

		application.Logger.Info("password reset", "email", email)
		return nil
	},
}

func requirePasswordFromStdin(cmd *cobra.Command) (string, error) {
	useStdin, _ := cmd.Flags().GetBool("password-stdin")
	if !useStdin {
		return "", fmt.Errorf("--password-stdin is required (pipe password via stdin)")
	}

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("reading password from stdin: %w", err)
		}
		return "", fmt.Errorf("no password provided on stdin")
	}
	password := strings.TrimRight(scanner.Text(), "\r\n")
	if password == "" {
		return "", fmt.Errorf("password cannot be empty")
	}
	return password, nil
}

func init() {
	adminCreateCmd.Flags().String("email", "", "user email address")
	adminCreateCmd.Flags().String("name", "", "user display name")
	adminCreateCmd.Flags().Bool("password-stdin", false, "read password from stdin")

	adminPromoteCmd.Flags().String("email", "", "user email address")

	adminResetPasswordCmd.Flags().String("email", "", "user email address")
	adminResetPasswordCmd.Flags().Bool("password-stdin", false, "read password from stdin")

	appCommand(adminCreateCmd)
	appCommand(adminPromoteCmd)
	appCommand(adminResetPasswordCmd)

	adminCmd.AddCommand(adminCreateCmd)
	adminCmd.AddCommand(adminPromoteCmd)
	adminCmd.AddCommand(adminResetPasswordCmd)

	rootCmd.AddCommand(adminCmd)
}
