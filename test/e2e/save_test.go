package integration

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/containers/podman/v2/pkg/rootless"
	. "github.com/containers/podman/v2/test/utils"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Podman save", func() {
	var (
		tempdir    string
		err        error
		podmanTest *PodmanTestIntegration
	)

	BeforeEach(func() {
		tempdir, err = CreateTempDirInTempDir()
		if err != nil {
			os.Exit(1)
		}
		podmanTest = PodmanTestCreate(tempdir)
		podmanTest.Setup()
		podmanTest.RestoreAllArtifacts()
	})

	AfterEach(func() {
		podmanTest.Cleanup()
		f := CurrentGinkgoTestDescription()
		processTestResult(f)

	})

	It("podman save output flag", func() {
		outfile := filepath.Join(podmanTest.TempDir, "alpine.tar")

		save := podmanTest.PodmanNoCache([]string{"save", "-o", outfile, ALPINE})
		save.WaitWithDefaultTimeout()
		Expect(save.ExitCode()).To(Equal(0))
	})

	It("podman save oci flag", func() {
		outfile := filepath.Join(podmanTest.TempDir, "alpine.tar")

		save := podmanTest.PodmanNoCache([]string{"save", "-o", outfile, "--format", "oci-archive", ALPINE})
		save.WaitWithDefaultTimeout()
		Expect(save.ExitCode()).To(Equal(0))
	})

	It("podman save with stdout", func() {
		Skip("Pipe redirection in ginkgo probably won't work")
		outfile := filepath.Join(podmanTest.TempDir, "alpine.tar")

		save := podmanTest.PodmanNoCache([]string{"save", ALPINE, ">", outfile})
		save.WaitWithDefaultTimeout()
		Expect(save.ExitCode()).To(Equal(0))
	})

	It("podman save quiet flag", func() {
		outfile := filepath.Join(podmanTest.TempDir, "alpine.tar")

		save := podmanTest.PodmanNoCache([]string{"save", "-q", "-o", outfile, ALPINE})
		save.WaitWithDefaultTimeout()
		Expect(save.ExitCode()).To(Equal(0))
	})

	It("podman save bogus image", func() {
		outfile := filepath.Join(podmanTest.TempDir, "alpine.tar")

		save := podmanTest.PodmanNoCache([]string{"save", "-o", outfile, "FOOBAR"})
		save.WaitWithDefaultTimeout()
		Expect(save).To(ExitWithError())
	})

	It("podman save to directory with oci format", func() {
		if rootless.IsRootless() && podmanTest.RemoteTest {
			Skip("Requires a fix in containers image for chown/lchown")
		}
		outdir := filepath.Join(podmanTest.TempDir, "save")

		save := podmanTest.PodmanNoCache([]string{"save", "--format", "oci-dir", "-o", outdir, ALPINE})
		save.WaitWithDefaultTimeout()
		Expect(save.ExitCode()).To(Equal(0))
	})

	It("podman save to directory with v2s2 docker format", func() {
		if rootless.IsRootless() && podmanTest.RemoteTest {
			Skip("Requires a fix in containers image for chown/lchown")
		}
		outdir := filepath.Join(podmanTest.TempDir, "save")

		save := podmanTest.PodmanNoCache([]string{"save", "--format", "docker-dir", "-o", outdir, ALPINE})
		save.WaitWithDefaultTimeout()
		Expect(save.ExitCode()).To(Equal(0))
	})

	It("podman save to directory with docker format and compression", func() {
		if rootless.IsRootless() && podmanTest.RemoteTest {
			Skip("Requires a fix in containers image for chown/lchown")
		}
		outdir := filepath.Join(podmanTest.TempDir, "save")

		save := podmanTest.PodmanNoCache([]string{"save", "--compress", "--format", "docker-dir", "-o", outdir, ALPINE})
		save.WaitWithDefaultTimeout()
		Expect(save.ExitCode()).To(Equal(0))
	})

	It("podman save bad filename", func() {
		outdir := filepath.Join(podmanTest.TempDir, "save:colon")

		save := podmanTest.PodmanNoCache([]string{"save", "--compress", "--format", "docker-dir", "-o", outdir, ALPINE})
		save.WaitWithDefaultTimeout()
		Expect(save).To(ExitWithError())
	})

	It("podman save remove signature", func() {
		SkipIfRootless("FIXME: Need get in rootless push sign")
		if podmanTest.Host.Arch == "ppc64le" {
			Skip("No registry image for ppc64le")
		}
		tempGNUPGHOME := filepath.Join(podmanTest.TempDir, "tmpGPG")
		err := os.Mkdir(tempGNUPGHOME, os.ModePerm)
		Expect(err).To(BeNil())
		origGNUPGHOME := os.Getenv("GNUPGHOME")
		err = os.Setenv("GNUPGHOME", tempGNUPGHOME)
		Expect(err).To(BeNil())
		defer os.Setenv("GNUPGHOME", origGNUPGHOME)

		port := 5000
		session := podmanTest.Podman([]string{"run", "-d", "--name", "registry", "-p", strings.Join([]string{strconv.Itoa(port), strconv.Itoa(port)}, ":"), "quay.io/libpod/registry:2.6"})
		session.WaitWithDefaultTimeout()
		Expect(session.ExitCode()).To(Equal(0))
		if !WaitContainerReady(podmanTest, "registry", "listening on", 20, 1) {
			Skip("Cannot start docker registry.")
		}

		cmd := exec.Command("gpg", "--import", "sign/secret-key.asc")
		err = cmd.Run()
		Expect(err).To(BeNil())

		cmd = exec.Command("cp", "/etc/containers/registries.d/default.yaml", "default.yaml")
		if err = cmd.Run(); err != nil {
			Skip("no signature store to verify")
		}
		defer func() {
			cmd = exec.Command("cp", "default.yaml", "/etc/containers/registries.d/default.yaml")
			cmd.Run()
		}()

		cmd = exec.Command("cp", "sign/key.gpg", "/tmp/key.gpg")
		Expect(cmd.Run()).To(BeNil())
		sigstore := `
default-docker:
  sigstore: file:///var/lib/containers/sigstore
  sigstore-staging: file:///var/lib/containers/sigstore
`
		Expect(ioutil.WriteFile("/etc/containers/registries.d/default.yaml", []byte(sigstore), 0755)).To(BeNil())

		session = podmanTest.Podman([]string{"tag", ALPINE, "localhost:5000/alpine"})
		session.WaitWithDefaultTimeout()
		Expect(session.ExitCode()).To(Equal(0))

		session = podmanTest.Podman([]string{"push", "--tls-verify=false", "--sign-by", "foo@bar.com", "localhost:5000/alpine"})
		session.WaitWithDefaultTimeout()
		Expect(session.ExitCode()).To(Equal(0))

		session = podmanTest.Podman([]string{"rmi", ALPINE, "localhost:5000/alpine"})
		session.WaitWithDefaultTimeout()
		Expect(session.ExitCode()).To(Equal(0))

		session = podmanTest.Podman([]string{"pull", "--tls-verify=false", "--signature-policy=sign/policy.json", "localhost:5000/alpine"})
		session.WaitWithDefaultTimeout()
		Expect(session.ExitCode()).To(Equal(0))

		outfile := filepath.Join(podmanTest.TempDir, "temp.tar")
		save := podmanTest.Podman([]string{"save", "remove-signatures=true", "-o", outfile, "localhost:5000/alpine"})
		save.WaitWithDefaultTimeout()
		Expect(save).To(ExitWithError())
	})

	It("podman save image with digest reference", func() {
		// pull a digest reference
		session := podmanTest.PodmanNoCache([]string{"pull", ALPINELISTDIGEST})
		session.WaitWithDefaultTimeout()
		Expect(session.ExitCode()).To(Equal(0))

		// save a digest reference should exit without error.
		outfile := filepath.Join(podmanTest.TempDir, "temp.tar")
		save := podmanTest.PodmanNoCache([]string{"save", "-o", outfile, ALPINELISTDIGEST})
		save.WaitWithDefaultTimeout()
		Expect(save.ExitCode()).To(Equal(0))
	})

	It("podman save --multi-image-archive (tagged images)", func() {
		multiImageSave(podmanTest, RESTORE_IMAGES)
	})

	It("podman save --multi-image-archive (untagged images)", func() {
		// Refer to images via ID instead of tag.
		session := podmanTest.PodmanNoCache([]string{"images", "--format", "{{.ID}}"})
		session.WaitWithDefaultTimeout()
		Expect(session.ExitCode()).To(Equal(0))
		ids := session.OutputToStringArray()

		Expect(len(RESTORE_IMAGES), len(ids))
		multiImageSave(podmanTest, ids)
	})
})

// Create a multi-image archive, remove all images, load it and
// make sure that all images are (again) present.
func multiImageSave(podmanTest *PodmanTestIntegration, images []string) {
	// Create the archive.
	outfile := filepath.Join(podmanTest.TempDir, "temp.tar")
	session := podmanTest.PodmanNoCache(append([]string{"save", "-o", outfile, "--multi-image-archive"}, images...))
	session.WaitWithDefaultTimeout()
	Expect(session.ExitCode()).To(Equal(0))

	// Remove all images.
	session = podmanTest.PodmanNoCache([]string{"rmi", "-af"})
	session.WaitWithDefaultTimeout()
	Expect(session.ExitCode()).To(Equal(0))

	// Now load the archive.
	session = podmanTest.PodmanNoCache([]string{"load", "-i", outfile})
	session.WaitWithDefaultTimeout()
	Expect(session.ExitCode()).To(Equal(0))
	// Grep for each image in the `podman load` output.
	for _, image := range images {
		found, _ := session.GrepString(image)
		Expect(found).Should(BeTrue())
	}

	// Make sure that each image has really been loaded.
	for _, image := range images {
		session = podmanTest.PodmanNoCache([]string{"image", "exists", image})
		session.WaitWithDefaultTimeout()
		Expect(session.ExitCode()).To(Equal(0))
	}
}
