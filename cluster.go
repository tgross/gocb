package gocb

import (
	"crypto/tls"
	"fmt"
	"github.com/couchbase/gocb/gocbcore"
	"net/http"
	"time"
)

type Cluster struct {
	spec                 connSpec
	connectTimeout       time.Duration
	serverConnectTimeout time.Duration
}

func Connect(connSpecStr string) (*Cluster, error) {
	spec := parseConnSpec(connSpecStr)
	if spec.Scheme == "" {
		spec.Scheme = "http"
	}
	if spec.Scheme != "couchbase" && spec.Scheme != "couchbases" && spec.Scheme != "http" {
		panic("Unsupported Scheme!")
	}
	csResolveDnsSrv(&spec)
	cluster := &Cluster{
		spec:                 spec,
		connectTimeout:       60000 * time.Millisecond,
		serverConnectTimeout: 7000 * time.Millisecond,
	}
	return cluster, nil
}

func (c *Cluster) ConnectTimeout() time.Duration {
	return c.connectTimeout
}
func (c *Cluster) SetConnectTimeout(timeout time.Duration) {
	c.connectTimeout = timeout
}
func (c *Cluster) ServerConnectTimeout() time.Duration {
	return c.serverConnectTimeout
}
func (c *Cluster) SetServerConnectTimeout(timeout time.Duration) {
	c.serverConnectTimeout = timeout
}

func specToHosts(spec connSpec) ([]string, []string, bool) {
	var memdHosts []string
	var httpHosts []string
	isHttpHosts := spec.Scheme == "http"
	isSslHosts := spec.Scheme == "couchbases"
	for _, specHost := range spec.Hosts {
		cccpPort := specHost.Port
		httpPort := specHost.Port
		if isHttpHosts || cccpPort == 0 {
			if !isSslHosts {
				cccpPort = 11210
			} else {
				cccpPort = 11207
			}
		}
		if !isHttpHosts || httpPort == 0 {
			if !isSslHosts {
				httpPort = 8091
			} else {
				httpPort = 18091
			}
		}

		memdHosts = append(memdHosts, fmt.Sprintf("%s:%d", specHost.Host, cccpPort))
		httpHosts = append(httpHosts, fmt.Sprintf("%s:%d", specHost.Host, httpPort))
	}

	return memdHosts, httpHosts, isSslHosts
}

func (c *Cluster) makeAgentConfig(bucket, password string) *gocbcore.AgentConfig {
	authFn := func(srv gocbcore.AuthClient, deadline time.Time) error {
		// Build PLAIN auth data
		userBuf := []byte(bucket)
		passBuf := []byte(password)
		authData := make([]byte, 1+len(userBuf)+1+len(passBuf))
		authData[0] = 0
		copy(authData[1:], userBuf)
		authData[1+len(userBuf)] = 0
		copy(authData[1+len(userBuf)+1:], passBuf)

		// Execute PLAIN authentication
		_, err := srv.ExecSaslAuth([]byte("PLAIN"), authData, deadline)

		return err
	}

	memdHosts, httpHosts, isSslHosts := specToHosts(c.spec)

	var tlsConfig *tls.Config
	if isSslHosts {
		tlsConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	}

	return &gocbcore.AgentConfig{
		MemdAddrs:            memdHosts,
		HttpAddrs:            httpHosts,
		TlsConfig:            tlsConfig,
		BucketName:           bucket,
		Password:             password,
		AuthHandler:          authFn,
		ConnectTimeout:       c.connectTimeout,
		ServerConnectTimeout: c.serverConnectTimeout,
	}
}

func (c *Cluster) OpenBucket(bucket, password string) (*Bucket, error) {
	agentConfig := c.makeAgentConfig(bucket, password)
	return createBucket(agentConfig)
}

func (c *Cluster) Manager(username, password string) *ClusterManager {
	_, httpHosts, isSslHosts := specToHosts(c.spec)
	var mgmtHosts []string

	for _, host := range httpHosts {
		if isSslHosts {
			mgmtHosts = append(mgmtHosts, "https://"+host)
		} else {
			mgmtHosts = append(mgmtHosts, "http://"+host)
		}
	}

	var tlsConfig *tls.Config
	if isSslHosts {
		tlsConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	}

	return &ClusterManager{
		hosts:    mgmtHosts,
		username: username,
		password: password,
		httpCli: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		},
	}
}

type StreamingBucket struct {
	client *gocbcore.Agent
}

func (b *StreamingBucket) IoRouter() *gocbcore.Agent {
	return b.client
}

func (c *Cluster) OpenStreamingBucket(streamName, bucket, password string) (*StreamingBucket, error) {
	cli, err := gocbcore.CreateDcpAgent(c.makeAgentConfig(bucket, password), streamName)
	if err != nil {
		return nil, err
	}

	return &StreamingBucket{
		client: cli,
	}, nil
}
