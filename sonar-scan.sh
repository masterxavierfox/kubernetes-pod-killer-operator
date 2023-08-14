#!/bin/sh
sonar-scanner \
  -Dproject.settings=sonar.properties \
  -Dsonar.sources=. \
  -Dsonar.host.url=https://sonarlint.com \
  -Dsonar.login=$SONAR_TOKEN \
  -Dsonar.projectKey=kuberntes-resource-manager \
  -Dsonar.qualitygate.wait=true