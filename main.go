package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	bolt "go.etcd.io/bbolt"
)

var (
	challengeBoxDockerImage    string
	challengeBoxDockerCmd      string
	challengeBoxDockerPort     int
	challengeBoxDockerLifespan int
	httpServerListener         string

	db           *bolt.DB
	dockerClient *client.Client
	ctx          = context.Background()
)

func init() {
	// Set program flags
	flag.StringVar(&challengeBoxDockerImage, "image", "ubuntu", "Docker image")
	flag.StringVar(&challengeBoxDockerCmd, "cmd", "/usr/sbin/sshd", "Docker command to be send to background")
	flag.IntVar(&challengeBoxDockerPort, "port", 22, "Container exposed port to dynamically map on the host")
	flag.IntVar(&challengeBoxDockerLifespan, "life", 60, "Challenge lifetime before the box closes")
	flag.StringVar(&httpServerListener, "listen", "0.0.0.0:8080", "Address:Port the http server will bind to")

	// check docker requirement
	_, err := exec.LookPath("docker")
	if err != nil {
		log.Fatalf("Error Docker not found : %s", err)
	}

	// Instantiate a BBolt Database with a bucket dedicated for the configuration
	db, err = bolt.Open("./state.db", 0600, nil)
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

	dockerClient, err = client.NewClientWithOpts(client.WithVersion("1.39"))
	if err != nil {
		panic(err)
	}

}

func createNewChallengeBox(box, cmd string, duration, port int) (containerID []byte, err error) {
	containerIDDirty, err := exec.Command(
		"docker", "container", "run",
		"--detach", "--rm",
		"--publish", fmt.Sprintf("%d", port),
		fmt.Sprintf("%s", box),
		"sh", "-c",
		fmt.Sprintf("%s && sleep %d", cmd, duration)).Output()
	containerID = bytes.TrimSpace(containerIDDirty)
	return
}

func getHostSSHPort(containerID []byte) (port []byte, err error) {
	port, err = exec.Command(
		"docker", "inspect",
		"-f", "{{range $p, $conf := .NetworkSettings.Ports}} {{(index $conf 0).HostPort}} {{end}}",
		fmt.Sprintf("%s", containerID)).Output()
	return
}

func provideChallengeBox(w http.ResponseWriter, r *http.Request) {
	db, err := bolt.Open("./state.db", 0600, nil)
	if err != nil {
		log.Fatalf("Error creating Bbolt DB : %s", err)
	}
	defer db.Close()

	var srcIP string
	srcIP = strings.Split(r.RemoteAddr, ":")[0]

	db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("State"))
		containerID := b.Get([]byte(srcIP))

		if containerID == nil {
			log.Printf("Source IP %s is not known: creating a new challenge box.", srcIP)
			boxID, err := createNewChallengeBox(challengeBoxDockerImage, challengeBoxDockerCmd, challengeBoxDockerLifespan, challengeBoxDockerPort)
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

			fmt.Fprintf(w, "A new challenge box has been created : available via SSH for %d seconds on port %s", challengeBoxDockerLifespan, sshPort)

		} else {
			log.Printf("Source IP %s has already a challenge box : %s", srcIP, containerID)
			sshPort, err := getHostSSHPort(containerID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			log.Printf("The port associated with SSH in the box is %s", sshPort)

			fmt.Fprintf(w, "Picking an existing Challenge box on port %s", sshPort)

		}
		return nil
	})

}

func cleanDB() error {
	db, err := bolt.Open("./state.db", 0600, nil)
	if err != nil {
		log.Printf("Error creating Bbolt DB : %s", err)
		return err
	}
	defer db.Close()

	containers, err := dockerClient.ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		log.Printf("Error getting running containers : %s", err)
		return err
	}

	runningContainers := make([]string, len(containers))
	for _, c := range containers {
		runningContainers = append(runningContainers, c.ID)
	}
	fmt.Printf("%++v\n", runningContainers)

	err = db.Update(func(tx *bolt.Tx) error {
		// Assume bucket exists
		bucket := tx.Bucket([]byte("State"))
		cursor := bucket.Cursor()

		for ip, id := cursor.First(); ip != nil; ip, id = cursor.Next() {
			// fmt.Printf("key=%s, value=%s\n", ip, id)
			stillExist := false
			for _, cID := range runningContainers {
				if strings.Compare(cID, fmt.Sprintf("%s", id)) == 0 {
					stillExist = true
				}
			}
			if stillExist {
				log.Printf("IP %s still associated with container %s", ip, id)
			} else {
				err = bucket.Delete(ip)
				if err != nil {
					return err
				}
				log.Printf("IP %s removed from database", ip)
			}
		}
		return nil
	})
	return err
}

func main() {
	flag.Parse()

	go func() {
		for {
			log.Printf("DB cleaning started")
			cleanDB()
			// Wait for 10s.
			time.Sleep(10 * time.Second)
		}
	}()

	http.Handle("/", http.FileServer(http.Dir("./static")))
	http.HandleFunc("/create/", provideChallengeBox)

	log.Fatal(http.ListenAndServe(httpServerListener, nil))

}
