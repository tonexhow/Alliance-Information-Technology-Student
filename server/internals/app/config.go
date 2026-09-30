package app

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Port               string
	DatabaseURL        string
	PublicURL          string
	ClientURL          string
	SMTPEmail          string
	SMTPPassword       string
	SMTPHost           string
	SMTPPort           string
	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURL  string
	GitHubClientID     string
	GitHubClientSecret string
	GitHubRedirectURL  string
	DeveloperUsername  string
	DeveloperName      string
	DeveloperPassword  string
}

func loadConfig() Config {
	// Runtime environment variables have the highest priority. Project-local
	// credentials are loaded next, then the shared [BOOT] Scripts fallback only
	// fills values that are still missing.
	protected := map[string]bool{}
	for _, pair := range os.Environ() {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
			protected[parts[0]] = true
		}
	}

	loadFirstEnv([]string{
		".env",    // project root when the service starts from AITS/
		"../.env", // project root when the service starts from AITS/server/
	}, protected)
	loadFirstEnv([]string{
		"../../[BOOT] Scripts/global.env", // server working directory
		"../[BOOT] Scripts/global.env",    // project root working directory
		"[BOOT] Scripts/global.env",       // monorepo/root working directory
	}, protected)

	publicURL := firstNonEmpty(os.Getenv("DOMAIN"), os.Getenv("PUBLIC_URL"), "https://aits.jpano.dev")
	clientURL := firstNonEmpty(os.Getenv("CLIENT_URL"), publicURL)
	port := firstNonEmpty(os.Getenv("SERVER_PORT"), "3013")
	return Config{
		Port:               port,
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		PublicURL:          publicURL,
		ClientURL:          clientURL,
		SMTPEmail:          os.Getenv("SMTP_EMAIL"),
		SMTPPassword:       os.Getenv("SMTP_PASSWORD"),
		SMTPHost:           firstNonEmpty(os.Getenv("SMTP_HOST"), "smtp.gmail.com"),
		SMTPPort:           firstNonEmpty(os.Getenv("SMTP_PORT"), "587"),
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleRedirectURL:  publicURL + "/api/auth/google/callback",
		GitHubClientID:     os.Getenv("GITHUB_CLIENT_ID"),
		GitHubClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
		GitHubRedirectURL:  publicURL + "/api/auth/github/callback",
		DeveloperUsername:  "tonybcc",
		DeveloperName:      "Tony BCC",
		DeveloperPassword:  "",
	}
}

func loadFirstEnv(paths []string, protected map[string]bool) {
	for _, path := range paths {
		if fileExists(path) {
			loadEnvFile(path, protected)
			return
		}
	}
}

func loadEnvFile(path string, protected map[string]bool) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key == "" || value == "" || protected[key] || strings.TrimSpace(os.Getenv(key)) != "" {
			continue
		}
		_ = os.Setenv(key, value)
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (c Config) DatabaseReady() bool           { return strings.TrimSpace(c.DatabaseURL) != "" }
func (c Config) GoogleOAuthReady() bool        { return c.GoogleClientID != "" && c.GoogleClientSecret != "" }
func (c Config) GitHubOAuthReady() bool        { return c.GitHubClientID != "" && c.GitHubClientSecret != "" }
func (c Config) SMTPReady() bool               { return c.SMTPEmail != "" && c.SMTPPassword != "" }
func (c Config) DeveloperBootstrapReady() bool { return strings.TrimSpace(c.DeveloperPassword) != "" }
func (c Config) String() string {
	return fmt.Sprintf("port=%s public=%s client=%s database=%t google_oauth=%t github_oauth=%t smtp=%t", c.Port, c.PublicURL, c.ClientURL, c.DatabaseReady(), c.GoogleOAuthReady(), c.GitHubOAuthReady(), c.SMTPReady())
}
