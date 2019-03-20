package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	bolt "go.etcd.io/bbolt"
)

func createNewChallengeBox(duration int) (containerID []byte, err error) {
	containerIDDirty, err := exec.Command("docker", "run", "-d", "--rm", "-p", "22", "ubuntu", "sleep", fmt.Sprintf("%d", duration)).Output()
	containerID = bytes.TrimSpace(containerIDDirty)
	return
}

func getHostSSHPort(containerID []byte) (port []byte, err error) {
	port, err = exec.Command("docker", "inspect", "-f", "{{range $p, $conf := .NetworkSettings.Ports}} {{(index $conf 0).HostPort}} {{end}}", fmt.Sprintf("%s", containerID)).Output()
	return
}

func provideChallengeBox(w http.ResponseWriter, r *http.Request) {
	boxLifetime := 60
	db, err := bolt.Open("./state.db", 0600, nil)
	if err != nil {
		log.Fatalf("Error creating Bbolt DB : %s", err)
	}
	defer db.Close()

	srcIP := r.RemoteAddr

	db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("State"))
		containerID := b.Get([]byte(srcIP))

		if containerID == nil {
			log.Printf("Source IP %s is not known: creating a new challenge box.", srcIP)
			boxID, err := createNewChallengeBox(boxLifetime)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			db.Update(func(tx *bolt.Tx) error {
				b := tx.Bucket([]byte("State"))
				err := b.Put([]byte(srcIP), boxID)
				return err
			})

			sshPort, err := getHostSSHPort(boxID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}

			fmt.Fprintf(w, "A new challenge box has been created : available via SSH for %d seconds on port %s", boxLifetime, sshPort)

		} else {
			log.Printf("Source IP %s has already a challenge box : %s", srcIP, containerID)
			sshPort, err := getHostSSHPort(containerID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			log.Printf("The port associated with SSH in the box is %s", sshPort)

			fmt.Fprintf(w, "Picking an existing Challenge box : available via SSH for %d seconds on port %s", boxLifetime, sshPort)

		}
		return nil
	})

}

func main() {
	_, err := exec.LookPath("docker")
	if err != nil {
		log.Fatalf("Error Docker not found : %s", err)
	}
	db, err := bolt.Open("./state.db", 0600, nil)
	if err != nil {
		log.Fatalf("Error creating Bbolt DB : %s", err)
	}

	db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("State"))
		if err != nil {
			return fmt.Errorf("create bucket: %s", err)
		}
		return nil
	})
	db.Close()

	go func() {
		ctx := context.Background()
		dockerClient, err := client.NewClientWithOpts(client.WithVersion("1.39"))
		if err != nil {
			panic(err)
		}

		for {
			// Wait for 10s.
			time.Sleep(10 * time.Second)

			log.Printf("DB cleaning started")

			containers, err := dockerClient.ContainerList(ctx, types.ContainerListOptions{})
			if err != nil {
				panic(err)
			}
			db.View(func(tx *bolt.Tx) error {
				// Assume bucket exists and has keys
				//b := tx.Bucket([]byte("State"))
				// TODO check whether DB contains a containerid in the slice containers
				return nil
			})

			fmt.Printf("%v\n", containers)
		}

	}()

	http.HandleFunc("/create/", provideChallengeBox)

	log.Fatal(http.ListenAndServe(":8080", nil))

}
