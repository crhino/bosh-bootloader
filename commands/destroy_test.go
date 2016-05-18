package commands_test

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/pivotal-cf-experimental/bosh-bootloader/aws/cloudformation"
	"github.com/pivotal-cf-experimental/bosh-bootloader/boshinit"
	"github.com/pivotal-cf-experimental/bosh-bootloader/commands"
	"github.com/pivotal-cf-experimental/bosh-bootloader/fakes"
	"github.com/pivotal-cf-experimental/bosh-bootloader/storage"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("Destroy", func() {
	var (
		destroy               commands.Destroy
		boshDeleter           *fakes.BOSHDeleter
		stackManager          *fakes.StackManager
		infrastructureManager *fakes.InfrastructureManager
		vpcStatusChecker      *fakes.VPCStatusChecker
		stringGenerator       *fakes.StringGenerator
		logger                *fakes.Logger
		keyPairDeleter        *fakes.KeyPairDeleter
		stdin                 *bytes.Buffer
	)

	BeforeEach(func() {
		stdin = bytes.NewBuffer([]byte{})
		logger = &fakes.Logger{}

		vpcStatusChecker = &fakes.VPCStatusChecker{}
		stackManager = &fakes.StackManager{}
		infrastructureManager = &fakes.InfrastructureManager{}
		boshDeleter = &fakes.BOSHDeleter{}
		keyPairDeleter = &fakes.KeyPairDeleter{}
		stringGenerator = &fakes.StringGenerator{}

		destroy = commands.NewDestroy(logger, stdin, boshDeleter, vpcStatusChecker, stackManager, stringGenerator, infrastructureManager, keyPairDeleter)
	})

	Describe("Execute", func() {
		DescribeTable("prompting the user for confirmation",
			func(response string, proceed bool) {
				fmt.Fprintf(stdin, "%s\n", response)

				_, err := destroy.Execute(commands.GlobalFlags{}, []string{}, storage.State{})
				Expect(err).NotTo(HaveOccurred())

				Expect(logger.PromptCall.Receives.Message).To(Equal("Are you sure you want to delete your infrastructure? This operation cannot be undone!"))

				if proceed {
					Expect(logger.StepCall.Receives.Message).To(Equal("destroying BOSH director and AWS stack"))
					Expect(boshDeleter.DeleteCall.CallCount).To(Equal(1))
				} else {
					Expect(logger.StepCall.Receives.Message).To(Equal("exiting"))
					Expect(boshDeleter.DeleteCall.CallCount).To(Equal(0))
				}
			},
			Entry("responding with 'yes'", "yes", true),
			Entry("responding with 'y'", "y", true),
			Entry("responding with 'Yes'", "Yes", true),
			Entry("responding with 'Y'", "Y", true),
			Entry("responding with 'no'", "no", false),
			Entry("responding with 'n'", "n", false),
			Entry("responding with 'No'", "No", false),
			Entry("responding with 'N'", "N", false),
		)

		Context("when the --no-confirm flag is supplied", func() {
			DescribeTable("destroys without prompting the user for confirmation", func(flag string) {
				_, err := destroy.Execute(commands.GlobalFlags{}, []string{flag}, storage.State{})
				Expect(err).NotTo(HaveOccurred())

				Expect(logger.PromptCall.CallCount).To(Equal(0))
				Expect(boshDeleter.DeleteCall.CallCount).To(Equal(1))
			},
				Entry("--no-confirm", "--no-confirm"),
				Entry("-n", "-n"),
			)
		})

		Describe("destroying the infrastructure", func() {
			var (
				state storage.State
				flags commands.GlobalFlags
			)

			BeforeEach(func() {
				stdin.Write([]byte("yes\n"))
				flags = commands.GlobalFlags{
					EndpointOverride: "some-endpoint",
				}
				state = storage.State{
					AWS: storage.AWS{
						AccessKeyID:     "some-access-key-id",
						SecretAccessKey: "some-secret-access-key",
						Region:          "some-aws-region",
					},
					KeyPair: storage.KeyPair{
						Name:       "some-ec2-key-pair-name",
						PrivateKey: "some-private-key",
						PublicKey:  "some-public-key",
					},
					BOSH: storage.BOSH{
						DirectorUsername: "some-director-username",
						DirectorPassword: "some-director-password",
						State: map[string]interface{}{
							"key": "value",
						},
						Credentials: map[string]string{
							"some-username": "some-password",
						},
						DirectorSSLCertificate: "some-certificate",
						DirectorSSLPrivateKey:  "some-private-key",
						Manifest:               "bosh-init-manifest",
					},
					Stack: storage.Stack{
						Name: "some-stack-name",
					},
				}
			})

			It("fails fast if BOSH deployed VMs still exist in the VPC", func() {
				stackManager.DescribeCall.Returns.Stack = cloudformation.Stack{
					Name:   "some-stack-name",
					Status: "some-stack-status",
					Outputs: map[string]string{
						"VPCID": "some-vpc-id",
					},
				}
				vpcStatusChecker.ValidateSafeToDeleteCall.Returns.Error = errors.New("vpc some-vpc-id is not safe to delete")

				_, err := destroy.Execute(flags, []string{}, state)
				Expect(err).To(MatchError("vpc some-vpc-id is not safe to delete"))

				Expect(vpcStatusChecker.ValidateSafeToDeleteCall.Receives.VPCID).To(Equal("some-vpc-id"))
			})

			It("invokes bosh-init delete", func() {
				stackManager.DescribeCall.Returns.Stack = cloudformation.Stack{
					Name:   "some-stack-name",
					Status: "some-stack-status",
					Outputs: map[string]string{
						"BOSHSubnet":              "some-subnet-id",
						"BOSHSubnetAZ":            "some-availability-zone",
						"BOSHEIP":                 "some-elastic-ip",
						"BOSHUserAccessKey":       "some-access-key-id",
						"BOSHUserSecretAccessKey": "some-secret-access-key",
						"BOSHSecurityGroup":       "some-security-group",
					},
				}

				_, err := destroy.Execute(flags, []string{}, state)
				Expect(err).NotTo(HaveOccurred())

				Expect(stackManager.DescribeCall.Receives.StackName).To(Equal("some-stack-name"))

				Expect(boshDeleter.DeleteCall.Receives.BOSHInitManifest).To(Equal("bosh-init-manifest"))
				Expect(boshDeleter.DeleteCall.Receives.BOSHInitState).To(Equal(boshinit.State{"key": "value"}))
				Expect(boshDeleter.DeleteCall.Receives.EC2PrivateKey).To(Equal("some-private-key"))
			})

			It("deletes the stack", func() {
				_, err := destroy.Execute(flags, []string{}, state)
				Expect(err).NotTo(HaveOccurred())

				Expect(infrastructureManager.DeleteCall.Receives.StackName).To(Equal("some-stack-name"))
			})

			It("deletes the keypair", func() {
				_, err := destroy.Execute(flags, []string{}, state)
				Expect(err).NotTo(HaveOccurred())

				Expect(keyPairDeleter.DeleteCall.Receives.Name).To(Equal("some-ec2-key-pair-name"))
			})

			It("clears the state", func() {
				state, err := destroy.Execute(flags, []string{}, state)
				Expect(err).NotTo(HaveOccurred())
				Expect(state).To(Equal(storage.State{}))
			})
		})

		Context("failure cases", func() {
			BeforeEach(func() {
				stdin.Write([]byte("yes\n"))
			})

			Context("when an invalid command line flag is supplied", func() {
				It("returns an error", func() {
					_, err := destroy.Execute(commands.GlobalFlags{}, []string{"--invalid-flag"}, storage.State{})
					Expect(err).To(MatchError("flag provided but not defined: -invalid-flag"))
				})
			})

			Context("when the bosh delete fails", func() {
				It("returns an error", func() {
					boshDeleter.DeleteCall.Returns.Error = errors.New("BOSH Delete Failed")

					_, err := destroy.Execute(commands.GlobalFlags{}, []string{}, storage.State{})
					Expect(err).To(MatchError("BOSH Delete Failed"))
				})
			})

			Context("when the stack manager cannot describe the stack", func() {
				It("returns an error", func() {
					stackManager.DescribeCall.Returns.Error = errors.New("cannot describe stack")

					_, err := destroy.Execute(commands.GlobalFlags{}, []string{}, storage.State{})
					Expect(err).To(MatchError("cannot describe stack"))
				})
			})

			Context("when failing to delete the stack", func() {
				It("returns an error", func() {
					infrastructureManager.DeleteCall.Returns.Error = errors.New("failed to delete stack")

					_, err := destroy.Execute(commands.GlobalFlags{}, []string{}, storage.State{})
					Expect(err).To(MatchError("failed to delete stack"))
				})
			})

			Context("when the keypair cannot be deleted", func() {
				It("returns an error", func() {
					keyPairDeleter.DeleteCall.Returns.Error = errors.New("failed to delete keypair")

					_, err := destroy.Execute(commands.GlobalFlags{}, []string{}, storage.State{})
					Expect(err).To(MatchError("failed to delete keypair"))
				})
			})
		})
	})
})
