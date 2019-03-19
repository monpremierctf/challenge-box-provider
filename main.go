package main

import (
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strings"
)

func createNewChallengeBox(duration int) (containerID string, err error) {
	createdContainer, err := exec.Command("docker", "run", "-d", "--rm", "-p", "22", "ubuntu", "sleep", fmt.Sprintf("%d", duration)).Output()
	if err != nil {
		return "", err
	}
	containerID = strings.TrimSpace(fmt.Sprintf("%s", createdContainer))

	return
}

func getHostSSHPort(containerID string) (string, error) {
	port, err := exec.Command("docker", "inspect", "-f", "{{range $p, $conf := .NetworkSettings.Ports}} {{$p}} -> {{(index $conf 0).HostPort}} {{end}}", fmt.Sprintf("%s", containerID)).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(fmt.Sprintf("%s", port)), nil
}

func provideChallengeBox(w http.ResponseWriter, r *http.Request) {
	boxLifetime := 60
	boxID, err := createNewChallengeBox(boxLifetime)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	fmt.Printf("%s\n", boxID)
	sshPort, err := getHostSSHPort(boxID)

	fmt.Fprintf(w, "Challenge box available via SSH for %d seconds on port %s", boxLifetime, sshPort)
}

func main() {
	_, err := exec.LookPath("docker")
	if err != nil {
		log.Fatalf("Error Docker not found : %s", err)
	}

	http.HandleFunc("/create/", provideChallengeBox)

	log.Fatal(http.ListenAndServe(":8080", nil))

}
