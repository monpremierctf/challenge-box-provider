package main

import (
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strings"
)

func createNewChallengeBox(duration int) (containerID string, err error) {
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return "", err
	}

	createdContainer, err := exec.Command(dockerPath, "run", "-d", "--rm", "-p", "22", "ubuntu", "sleep", fmt.Sprintf("%d", duration)).Output()
	if err != nil {
		return "", err
	}
	containerID = strings.TrimSpace(fmt.Sprintf("%s", createdContainer))

	return
}

func provideChallengeBox(w http.ResponseWriter, r *http.Request) {
	boxID, err := createNewChallengeBox(60)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	fmt.Printf("%s\n", boxID)

	fmt.Fprintf(w, "Challenge box available via SSH on port %s", boxID)
}

func main() {

	http.HandleFunc("/create/", provideChallengeBox)

	log.Fatal(http.ListenAndServe(":8080", nil))

}
