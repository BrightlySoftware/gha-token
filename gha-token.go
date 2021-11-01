package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	logger "log"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"
	"time"

	jwt "github.com/golang-jwt/jwt"
	flag "github.com/spf13/pflag"
)

type installationToken struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

type installation struct {
	ID              int    `json:"id"`
	AccessTokensURL string `json:"access_tokens_url"`
	RepositoriesURL string `json:"repositories_url"`
}

type repositories struct {
	List []struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repositories"`
}

type config struct {
	apiURL    string
	appID     string
	keyPath   string
	installID string
	repoOwner string
	repoName  string
}

var verbose bool

func main() {
	var cfg = parseFlags()

	jwtToken, err := getJwtToken(cfg.appID, cfg.keyPath)
	handleErrorIfAny(err)

	var token string

	if cfg.installID == "" && cfg.repoOwner == "" {
		log("Generated JWT token for app ID %s\n", cfg.appID)
		token = jwtToken
	} else if cfg.installID != "" {
		installToken := getInstallationToken(cfg.apiURL, jwtToken, cfg.appID, cfg.installID)
		log("Generated installation token for app ID %s and installation ID %s that expires at %s\n", cfg.appID, cfg.installID, installToken.ExpiresAt)
		token = installToken.Token
	} else {
		installToken, err := getInstallationTokenForRepo(cfg.apiURL, jwtToken, cfg.appID, cfg.repoOwner, cfg.repoName)
		handleErrorIfAny(err)
		log("Generated installation token for app ID %s and repo %s/%s that expires at %s\n", cfg.appID, cfg.repoOwner, cfg.repoName, installToken.ExpiresAt)
		token = installToken.Token
	}

	fmt.Print(token)
}

func parseFlags() config {
	var cfg config

	flag.StringVarP(&cfg.apiURL, "apiUrl", "g", "https://api.github.com", "GitHub API URL")
	flag.StringVarP(&cfg.appID, "appId", "a", "", "Appliction ID as defined in app settings (Required)")
	flag.StringVarP(&cfg.keyPath, "keyPath", "k", "", "Path to key PEM file generated in app settings (Required)")
	flag.StringVarP(&cfg.installID, "installId", "i", "", "Installation ID of the application")
	repoPtr := flag.StringP("repo", "r", "", "{owner/repo} of the GitHub repository")
	flag.BoolVarP(&verbose, "verbose", "v", false, "Verbose stderr")

	flag.Parse()

	if len(os.Args) == 1 {
		usage("")
	}

	if cfg.appID == "" {
		usage("appId is required")
	}

	if cfg.keyPath == "" {
		usage("keyPath is required")
	}

	if *repoPtr != "" {
		repoInfo := strings.Split(*repoPtr, "/")
		if len(repoInfo) != 2 {
			usage("repo argument value must be owner/repo but was: " + *repoPtr)
		}
		cfg.repoOwner, cfg.repoName = repoInfo[0], repoInfo[1]
	}

	return cfg
}

func getJwtToken(appID string, keyPath string) (string, error) {
	keyBytes, err := ioutil.ReadFile(keyPath)
	if err != nil {
		return "", err
	}

	signKey, err := jwt.ParseRSAPrivateKeyFromPEM(keyBytes)
	if err != nil {
		return "", err
	}

	now := time.Now()
	// StandardClaims: https://pkg.go.dev/github.com/golang-jwt/jwt#StandardClaims
	// Issuer: iss, IssuedAt: iat, ExpiresAt: exp
	claims := &jwt.StandardClaims{
		Issuer:    appID,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(time.Minute * 10).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)

	jwtTokenString, err := token.SignedString(signKey)
	if err != nil {
		return "", err
	}

	return jwtTokenString, nil
}

func httpJSON(method string, url string, authorization string, result interface{}) {
	client := &http.Client{}

	req, err := http.NewRequest(method, url, nil)
	handleErrorIfAny(err)
	req.Header.Add("Authorization", authorization)
	req.Header.Add("Accept", "application/vnd.github.machine-man-preview+json")

	reqDump, err := httputil.DumpRequestOut(req, true)
	if err == nil {
		log("GitHub request:\n%s", string(reqDump))
	} else {
		log("Unable to log GitHub request: %s", err)
	}

	resp, err := client.Do(req)
	handleErrorIfAny(err)

	respDump, err := httputil.DumpResponse(resp, true)
	if err == nil {
		log("GitHub response:\n%s", string(respDump))
	} else {
		log("Unable to log GitHub response: %s", err)
	}

	defer resp.Body.Close()

	respData, err := ioutil.ReadAll(resp.Body)
	handleErrorIfAny(err)

	json.Unmarshal(respData, &result)

	log("%s", result)
}

func getInstallationToken(apiURL string, jwtToken string, appID string, installationID string) installationToken {
	var token installationToken
	httpJSON("POST", fmt.Sprintf("%s/app/installations/%s/access_tokens", apiURL, installationID), "Bearer "+jwtToken, &token)

	return token
}

func getInstallationTokenForRepo(apiURL string, jwtToken string, appID string, owner string, repo string) (installationToken, error) {
	var installations []installation
	httpJSON("GET", apiURL+"/app/installations", "Bearer "+jwtToken, &installations)

	for _, installation := range installations {
		var token installationToken
		httpJSON("POST", installation.AccessTokensURL, "Bearer "+jwtToken, &token)

		var repos repositories
		httpJSON("GET", installation.RepositoriesURL, "token "+token.Token, &repos)

		for _, repository := range repos.List {
			if owner == repository.Owner.Login && repo == repository.Name {
				return token, nil
			}
		}
	}
	var empty installationToken
	return empty, fmt.Errorf("Unable to find repository %s/%s in installations of app ID %s", owner, repo, appID)
}

func log(format string, v ...interface{}) {
	if verbose {
		logger.Printf(format, v...)
	}
}

func handleErrorIfAny(err error) {
	if err != nil {
		logger.Fatalln(err)
	}
}

func usage(msg string) {
	if msg != "" {
		fmt.Fprintf(os.Stderr, "ERROR: %s\n\n", msg)
	}
	fmt.Fprintf(os.Stderr, "Usage: gha-token [flags]\n\nFlags:\n")
	flag.PrintDefaults()
	os.Exit(1)
}
