/*
(c) 2017 SAP SE or an SAP affiliate company. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

Modifications Copyright (c) 2017 SAP SE or an SAP affiliate company. All rights reserved.
*/

// Package controller is used to provide the core functionalities of machine-controller-manager
package controller

import (
	"crypto/rand"
	"encoding/hex"
	"math/big"
	"strings"
	"time"

	"github.com/gardener/machine-controller-manager/pkg/driver"
	"k8s.io/klog"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	bootstraptokenapi "k8s.io/cluster-bootstrap/token/api"
	bootstraptokenutil "k8s.io/cluster-bootstrap/token/util"
)

const placeholder = "<<BOOTSTRAP_TOKEN>>"

func (c *controller) addBootstrapTokenToUserData(machineName string, driver driver.Driver) error {
	userData := driver.GetUserData()
	klog.V(4).Infof("Creating bootstrap token!")
	bootstrapTokenSecret, err := c.getBootstrapTokenOrCreateIfNotExist(machineName)
	if err != nil {
		return err
	}

	token := bootstraptokenutil.TokenFromIDAndSecret(
		string(bootstrapTokenSecret.Data[bootstraptokenapi.BootstrapTokenIDKey]),
		string(bootstrapTokenSecret.Data[bootstraptokenapi.BootstrapTokenSecretKey]),
	)
	klog.V(4).Infof("replacing placeholder %s with %s in user-data!", placeholder, token)
	userData = strings.ReplaceAll(userData, placeholder, token)

	driver.SetUserData(userData)
	return nil
}

func (c *controller) getBootstrapTokenOrCreateIfNotExist(machineName string) (secret *v1.Secret, err error) {
	tokenID, secretName := getTokenIDAndSecretName(machineName)

	secret, err = c.targetCoreClient.CoreV1().Secrets(metav1.NamespaceSystem).Get(secretName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			bootstrapTokenSecretKey, err := generateRandomStringFromCharset(16, "0123456789abcdefghijklmnopqrstuvwxyz")
			if err != nil {
				return nil, err
			}
			data := map[string][]byte{
				bootstraptokenapi.BootstrapTokenDescriptionKey:      []byte("A bootstrap token generated by MachineControllManager."),
				bootstraptokenapi.BootstrapTokenIDKey:               []byte(tokenID),
				bootstraptokenapi.BootstrapTokenSecretKey:           []byte(bootstrapTokenSecretKey),
				bootstraptokenapi.BootstrapTokenExpirationKey:       []byte(metav1.Now().Add(c.safetyOptions.MachineCreationTimeout.Duration).Format(time.RFC3339)),
				bootstraptokenapi.BootstrapTokenUsageAuthentication: []byte("true"),
				bootstraptokenapi.BootstrapTokenUsageSigningKey:     []byte("true"),
			}

			secret = &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: metav1.NamespaceSystem,
				},
				Type: bootstraptokenapi.SecretTypeBootstrapToken,
				Data: data,
			}

			return c.targetCoreClient.CoreV1().Secrets(metav1.NamespaceSystem).Create(secret)
		}
		return nil, err
	}

	return secret, nil
}

func (c *controller) deleteBootstrapToken(machineName string) error {
	_, secretName := getTokenIDAndSecretName(machineName)
	err := c.targetCoreClient.CoreV1().Secrets(metav1.NamespaceSystem).Delete(secretName, &metav1.DeleteOptions{})
	if err != nil && apierrors.IsNotFound(err) {
		// Object no longer exists and has been deleted
		return nil
	}
	return err
}

// generateRandomStringFromCharset generates a cryptographically secure random string of the specified length <n>.
// The set of allowed characters can be specified. Returns error if there was a problem during the random generation.
func generateRandomStringFromCharset(n int, allowedCharacters string) (string, error) {
	output := make([]byte, n)
	max := new(big.Int).SetInt64(int64(len(allowedCharacters)))
	for i := range output {
		randomCharacter, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		output[i] = allowedCharacters[randomCharacter.Int64()]
	}
	return string(output), nil
}

func getTokenIDAndSecretName(machineName string) (string, string) {
	tokenID := hex.EncodeToString([]byte(machineName)[len(machineName)-5:])[:6]
	secretName := bootstraptokenutil.BootstrapTokenSecretName(tokenID)
	return tokenID, secretName
}
