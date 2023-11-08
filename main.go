package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/aws-iam-authenticator/pkg/token"
)

func CreateKubernetesClientSet(clusterName string, region string) *kubernetes.Clientset {
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(region))
	if err != nil {
		panic(err)
	}
	slog.Debug("Creating EKS client")

	eksClient := eks.NewFromConfig(cfg)

	slog.Debug(clusterName)

	cluster, err := eksClient.DescribeCluster(context.TODO(), &eks.DescribeClusterInput{
		Name: aws.String(clusterName),
	})
	if err != nil {
		panic(err)
	}
	if cluster.Cluster == nil {
		panic(fmt.Errorf("cluster %s not found", clusterName))
	}

	gen, err := token.NewGenerator(true, false)
	if err != nil {
		slog.Error("error creating token generator: %v\n", err)
	}

	opts := &token.GetTokenOptions{
		ClusterID: *cluster.Cluster.Name,
	}

	tok, err := gen.GetWithOptions(opts)
	if err != nil {
		slog.Error("error getting token: %v\n", err)
	}

	ca, err := base64.StdEncoding.DecodeString(*cluster.Cluster.CertificateAuthority.Data)
	if err != nil {
		slog.Error("error decoding certificate authority data: %v\n", err)
	}

	clientset, err := kubernetes.NewForConfig(
		&rest.Config{
			Host:        *cluster.Cluster.Endpoint,
			BearerToken: tok.Token,
			TLSClientConfig: rest.TLSClientConfig{
				CAData: ca,
			},
		},
	)

	if err != nil {
		slog.Error("error creating kubernetes client: %v\n", err)
		panic(err)
	}

	return clientset
}

func main() {

	clusterName := os.Getenv("CLUSTER_NAME")
	region := os.Getenv("AWS_REGION")

	clientSet := CreateKubernetesClientSet(clusterName, region)

	pods, err := clientSet.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		panic(err)
	}

	for _, pod := range pods.Items {
		fmt.Printf("Pod name: %s\n", pod.Name)
	}
}
