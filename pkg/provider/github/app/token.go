package app

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v4"
	gt "github.com/google/go-github/v61/github"
	"github.com/openshift-pipelines/pipelines-as-code/pkg/apis/pipelinesascode/v1alpha1"
	"github.com/openshift-pipelines/pipelines-as-code/pkg/params"
	"github.com/openshift-pipelines/pipelines-as-code/pkg/provider/github"
	"knative.dev/pkg/logging"
)

type Install struct {
	request   *http.Request
	run       *params.Run
	repo      *v1alpha1.Repository
	ghClient  *github.Provider
	namespace string

	repoList []string
}

func NewInstallation(req *http.Request, run *params.Run, repo *v1alpha1.Repository, gh *github.Provider, namespace string) *Install {
	if req == nil {
		req = &http.Request{}
	}
	return &Install{
		request:   req,
		run:       run,
		repo:      repo,
		ghClient:  gh,
		namespace: namespace,
	}
}

func (ip *Install) GetAndUpdateInstallationID(ctx context.Context) (string, string, int64, error) {
	var (
		enterpriseHost, token string
		installationID        int64
	)
	jwtToken, err := ip.GenerateJWT(ctx)
	if err != nil {
		return "", "", 0, err
	}

	apiUrl := *ip.ghClient.APIURL
	enterpriseHost = ip.request.Header.Get("X-GitHub-Enterprise-Host")
	if enterpriseHost != "" {
		// NOTE: Hopefully this works even when the ghe URL is on another host than the api URL
		apiUrl = "https://" + enterpriseHost + "/api/v3"
	}

	/*
		installationURL := *ip.ghClient.APIURL + keys.InstallationURL
		enterpriseHost = ip.request.Header.Get("X-GitHub-Enterprise-Host")
		if enterpriseHost != "" {
			// NOTE: Hopefully this works even when the ghe URL is on another host than the api URL
			installationURL = "https://" + enterpriseHost + "/api/v3" + keys.InstallationURL
		}

		res, err := GetReponse(ctx, http.MethodGet, installationURL, jwtToken, ip.run)
		if err != nil {
			return "", "", 0, err
		}

		if res.StatusCode >= 300 {
			return "", "", 0, fmt.Errorf("Non-OK HTTP status while getting installation URL: %s : %d", installationURL, res.StatusCode)
		}

		defer res.Body.Close()
		data, err := io.ReadAll(res.Body)
		if err != nil {
			return "", "", 0, err
		}

		installationData := []gt.Installation{}
		if err = json.Unmarshal(data, &installationData); err != nil {
			return "", "", 0, err
		}

		fmt.Printf("DEBUG: number of installations: %d\n", len(installationData))
		fmt.Printf("DEBUG: installationData: %v\n", installationData)
	*/

	logger := logging.FromContext(ctx)
	if logger == nil {
		fmt.Printf("DEBUG: Got no logger")
	}
	// ena - use the GH Go Library
	opt := &gt.ListOptions{PerPage: ip.ghClient.PaginedNumber}
	client, _, _ := github.MakeClient(ctx, apiUrl, jwtToken)
	//fmt.Printf("DEBUG: client: %#v\n", client)
	installationData := []*gt.Installation{}
	for {
		installationSet, resp, err := client.Apps.ListInstallations(ctx, opt)
		if err != nil {
			return "", "", 0, err
		}
		for _, installation := range installationSet {
			installationData = append(installationData, installation)
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	fmt.Printf("DEBUG: ip instance: %#v\n", ip)
	fmt.Printf("DEBUG: *ip.ghClient instance parent 1: %#v\n", *ip.ghClient)
	fmt.Printf("DEBUG: number of installations: %d\n", len(installationData))
	//fmt.Printf("DEBUG: installationData: %v\n", installationData)

	/* each installationID can have list of repository
	ref: https://docs.github.com/en/developers/apps/building-github-apps/authenticating-with-github-apps#authenticating-as-an-installation ,
	     https://docs.github.com/en/rest/apps/installations?apiVersion=2022-11-28#list-repositories-accessible-to-the-app-installation */
	for i := range installationData {
		fmt.Printf("DEBUG: index: %d installation ID: %d\n", i, *installationData[i].ID)
		if installationData[i].ID == nil {
			return "", "", 0, fmt.Errorf("installation ID is nil")
		}
		if *installationData[i].ID != 0 {
			token, err = ip.ghClient.GetAppToken(ctx, ip.run.Clients.Kube, enterpriseHost, *installationData[i].ID, ip.namespace)
			// While looping on the list of installations, there could be cases where we can't
			// obtain a token for installation. In a test I did for GitHub App with ~400
			// installations, there were 3 failing consistently with:
			// "could not refresh installation id XXX's token: received non 2xx response status "403 Forbidden".
			// If there is a matching installation after the failure, we miss it. So instead of
			// failing, we just log the error and continue.
			if err != nil {
				// DEBUG: remove this
				//fmt.Printf("DEBUG: error is %s", err)
				//return "", "", 0, err
				logger.Warn(err)
				continue
			}
		}
		fmt.Printf("DEBUG: token parent: %#v - %s\n", *ip.ghClient.Token, token)
		exist, err := ip.listRepos(ctx)
		if err != nil {
			return "", "", 0, err
		}
		if exist {
			installationID = *installationData[i].ID
			fmt.Printf("DEBUG: EXISTS")
			break
		}
	}
	fmt.Printf("DEBUG: before exist installationID is %d, token is %s\n", installationID, token)
	return enterpriseHost, token, installationID, nil
}

func (ip *Install) listRepos(ctx context.Context) (bool, error) {
	/*
		fmt.Println("DEBUG: ip.repoList", ip.repoList)
		if ip.repoList == nil {
			fmt.Println("DEBUG: that should print just once")
			var err error
			ip.repoList, err = github.ListRepos(ctx, ip.ghClient)
			if err != nil {
				return false, err
			}
			fmt.Println("DEBUG: ip.repoList 2", ip.repoList)
		}
	*/
	installationRepoList, err := github.ListRepos(ctx, ip.ghClient)
	fmt.Println("DEBUG: len installationRepoList", len(installationRepoList))
	if err != nil {
		return false, err
	}
	fmt.Println("DEBUG: len ip.repoList", len(ip.repoList))
	ip.repoList = append(ip.repoList, installationRepoList...)
	for i := range installationRepoList {
		// If URL matches with repo spec url then we can break for loop
		if installationRepoList[i] == ip.repo.Spec.URL {
			return true, nil
		}
	}
	return false, nil
}

type JWTClaim struct {
	Issuer int64 `json:"iss"`
	jwt.RegisteredClaims
}

func (ip *Install) GenerateJWT(ctx context.Context) (string, error) {
	// TODO: move this out of here
	gh := github.New()
	gh.Run = ip.run
	applicationID, privateKey, err := gh.GetAppIDAndPrivateKey(ctx, ip.namespace, ip.run.Clients.Kube)
	if err != nil {
		return "", err
	}

	// The expirationTime claim identifies the expiration time on or after which the JWT MUST NOT be accepted for processing.
	// Value cannot be longer duration.
	// See https://datatracker.ietf.org/doc/html/rfc7519#section-4.1.4
	expirationTime := time.Now().Add(5 * time.Minute)
	claims := &JWTClaim{
		Issuer: applicationID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)

	parsedPK, err := jwt.ParseRSAPrivateKeyFromPEM(privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %w", err)
	}

	tokenString, err := token.SignedString(parsedPK)
	if err != nil {
		return "", fmt.Errorf("failed to sign private key: %w", err)
	}
	return tokenString, nil
}

/*
func GetReponse(ctx context.Context, method, urlData, jwtToken string, run *params.Run) (*http.Response, error) {
	rawurl, err := url.Parse(urlData)
	if err != nil {
		return nil, err
	}

	newreq, err := http.NewRequestWithContext(ctx, method, rawurl.String(), nil)
	if err != nil {
		return nil, err
	}
	newreq.Header = map[string][]string{
		"Accept":        {"application/vnd.github+json"},
		"Authorization": {fmt.Sprintf("Bearer %s", jwtToken)},
	}
	res, err := run.Clients.HTTP.Do(newreq)
	return res, err
}
*/
