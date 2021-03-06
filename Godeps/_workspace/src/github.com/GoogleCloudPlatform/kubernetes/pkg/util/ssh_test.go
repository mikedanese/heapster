/*
Copyright 2015 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package util

import (
	"fmt"
	"net"
	"reflect"
	"testing"

	"github.com/golang/glog"
	"golang.org/x/crypto/ssh"
)

type testSSHServer struct {
	Host       string
	Port       string
	Type       string
	Data       []byte
	PrivateKey []byte
	PublicKey  []byte
}

func runTestSSHServer(user, password string) (*testSSHServer, error) {
	result := &testSSHServer{}
	// Largely derived from https://godoc.org/golang.org/x/crypto/ssh#example-NewServerConn
	config := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == user && string(pass) == password {
				return nil, nil
			}
			return nil, fmt.Errorf("password rejected for %s", c.User())
		},
		PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			result.Type = key.Type()
			result.Data = ssh.MarshalAuthorizedKey(key)
			return nil, nil
		},
	}

	privateKey, publicKey, err := GenerateKey(2048)
	if err != nil {
		return nil, err
	}
	privateBytes := EncodePrivateKey(privateKey)
	signer, err := ssh.ParsePrivateKey(privateBytes)
	if err != nil {
		return nil, err
	}
	config.AddHostKey(signer)
	result.PrivateKey = privateBytes

	publicBytes, err := EncodePublicKey(publicKey)
	if err != nil {
		return nil, err
	}
	result.PublicKey = publicBytes

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	host, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return nil, err
	}
	result.Host = host
	result.Port = port
	go func() {
		// TODO: return this port.
		defer listener.Close()

		conn, err := listener.Accept()
		if err != nil {
			glog.Errorf("Failed to accept: %v", err)
		}
		_, chans, reqs, err := ssh.NewServerConn(conn, config)
		if err != nil {
			glog.Errorf("Failed handshake: %v", err)
		}
		go ssh.DiscardRequests(reqs)
		for newChannel := range chans {
			if newChannel.ChannelType() != "direct-tcpip" {
				newChannel.Reject(ssh.UnknownChannelType, fmt.Sprintf("unknown channel type: %s", newChannel.ChannelType()))
				continue
			}
			channel, requests, err := newChannel.Accept()
			if err != nil {
				glog.Errorf("Failed to accept channel: %v", err)
			}

			for req := range requests {
				glog.Infof("Got request: %v", req)
			}

			channel.Close()
		}
	}()
	return result, nil
}

func TestSSHTunnel(t *testing.T) {
	private, public, err := GenerateKey(2048)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
		t.FailNow()
	}
	server, err := runTestSSHServer("foo", "bar")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
		t.FailNow()
	}

	privateData := EncodePrivateKey(private)
	tunnel, err := NewSSHTunnelFromBytes("foo", privateData, server.Host)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
		t.FailNow()
	}
	tunnel.SSHPort = server.Port

	if err := tunnel.Open(); err != nil {
		t.Errorf("unexpected error: %v", err)
		t.FailNow()
	}

	_, err = tunnel.Dial("tcp", "127.0.0.1:8080")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if server.Type != "ssh-rsa" {
		t.Errorf("expected %s, got %s", "ssh-rsa", server.Type)
	}

	publicData, err := EncodeSSHKey(public)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(server.Data, publicData) {
		t.Errorf("expected %s, got %s", string(server.Data), string(privateData))
	}

	if err := tunnel.Close(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
