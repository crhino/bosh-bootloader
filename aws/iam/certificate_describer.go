package iam

import (
	"errors"
	"net/http"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	awsiam "github.com/aws/aws-sdk-go/service/iam"
)

var CertificateNotFound error = errors.New("certificate not found")
var CertificateDescriptionFailure error = errors.New("failed to describe certificate")

type CertificateDescriber struct {
	iamClient Client
}

func NewCertificateDescriber(iamClient Client) CertificateDescriber {
	return CertificateDescriber{
		iamClient: iamClient,
	}
}

func (c CertificateDescriber) Describe(certificateName string) (Certificate, error) {
	output, err := c.iamClient.GetServerCertificate(&awsiam.GetServerCertificateInput{
		ServerCertificateName: aws.String(certificateName),
	})

	if err != nil {
		if e, ok := err.(awserr.RequestFailure); ok {
			if e.StatusCode() == http.StatusNotFound && e.Code() == "NoSuchEntity" {
				return Certificate{}, CertificateNotFound
			}
		}
		return Certificate{}, err
	}

	if output.ServerCertificate == nil || output.ServerCertificate.ServerCertificateMetadata == nil {
		return Certificate{}, CertificateDescriptionFailure
	}

	return Certificate{
		Name: aws.StringValue(output.ServerCertificate.ServerCertificateMetadata.ServerCertificateName),
		Body: aws.StringValue(output.ServerCertificate.CertificateBody),
		ARN:  aws.StringValue(output.ServerCertificate.ServerCertificateMetadata.Arn),
	}, nil
}
