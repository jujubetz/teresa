# Teresa
[![Release](https://img.shields.io/github/release/luizalabs/teresa.svg?style=flat-square)](https://github.com/luizalabs/teresa/releases/latest)
[![Software License](https://img.shields.io/badge/license-apache-brightgreen.svg?style=flat-square)](/LICENSE.md)
[![Build Status](https://img.shields.io/travis/luizalabs/teresa/master.svg?style=flat-square)](https://travis-ci.org/luizalabs/teresa)
[![codecov](https://img.shields.io/codecov/c/github/luizalabs/teresa/master.svg?style=flat-square")](https://codecov.io/gh/luizalabs/teresa)
[![Go Report Card](https://goreportcard.com/badge/github.com/luizalabs/teresa?style=flat-square)](https://goreportcard.com/report/github.com/luizalabs/teresa)

Teresa is an extremely simple platform as a service that runs on top of [Kubernetes](https://github.com/kubernetes/kubernetes).
It uses a client-server model: the client sends high level commands (create application, deploy, etc.) to the server, which translates them to the Kubernetes API.

## Installation

Server requirements:

- A working Kubernetes cluster

- Storage to save build artifacts: AWS S3 or minio

- RSA keys for signing the authentication token

- Database backend to store teams and users. You probably want to persist your data, so make sure to install `MySQL` through: `helm install --name teresa stable/mysql`, otherwise teresa will use `sqlite` as the storage engine and you'll lose teams and users data when teresa's pod restarts

- Optional TLS certificate and encription key

We recommend you install Teresa using the [helm](https://github.com/kubernetes/helm) package manager. Following is a working example assuming you're running the Kubernetes cluster on AWS, on `us-east-1` region and is using `MySQL` to store Teresa's users and teams.

Make sure to replace the dummy AWS credentials with real ones, as well as review the other varibles you might want to customize to suit your needs, such as the S3 bucket name, AWS region and so on.

    $ openssl genrsa -out teresa.rsa
    $ export TERESA_RSA_PRIVATE=`base64 teresa.rsa`  # use base64 -w0 on Linux
    $ openssl rsa -in teresa.rsa -pubout > teresa.rsa.pub
    $ export TERESA_RSA_PUBLIC=`base64 teresa.rsa.pub`
    $ export AWS_ACCESS_KEY_ID=foo
    $ export AWS_SECRET_ACCESS_KEY=bar
    $ helm repo add luizalabs http://helm.k8s.magazineluiza.com
    $ helm install luizalabs/teresa \
        --set rsa.private=$TERESA_RSA_PRIVATE \
        --set rsa.public=$TERESA_RSA_PUBLIC \
        --set aws.key.access=$AWS_ACCESS_KEY_ID \
        --set aws.key.secret=$AWS_SECRET_ACCESS_KEY \
        --set aws.region=us-east-1 \
        --set aws.s3.bucket=teresa \
        --set db.name=teresa \
        --set db.hostname=dbhostname \
        --set db.username=teresa \
        --set db.password=xxxxxxxx

Look [here](./helm/README.md) for more information about helm options.

## QuickStart

Teresa has the concept of teams, which are just sets of users. An application
belongs to a team and all its users can perform all operations on it. There are
administrative users, which are just regular users with an admin flag set up and
only them can do user and team management.

To create an admin user you need access to the environment where the Teresa
server is running (often a Kubernetes POD in namespace `teresa`):

    $ export POD_NAME=$(kubectl get pods --namespace teresa -l "app=teresa" -o jsonpath="{.items[0].metadata.name}")
    $ kubectl exec $POD_NAME --namespace=teresa -it -- teresa-server create-super-user --email admin_email --password xxxxxxxx

Now you can start creating other users and teams. First, you need to get the
Teresa endpoint created by Kubernetes and configure the client (get it
[here](https://github.com/luizalabs/teresa/releases/latest)):

    $ teresa config set-cluster mycluster --server <teresa-endpoint>

This creates a new cluster called `mycluster` and sets it as the current one.
Log in and create another user and a team:

    $ teresa login --user admin_email
    $ teresa team create myteam --email myemail
    $ teresa create user --name myname --email myemail --password xxxxxxxx
    $ teresa team add-user --team myteam --user myemail

This new user is able to create and deploy applications on behalf of the team:

    $ teresa login --user myemail
    $ teresa app create myapp --team myteam
    $ teresa deploy /path/to/myapp --app myapp --description "release 1.0"

Teresa has an extensive help builtin, you can access it with:

    $ teresa --help

Check out some examples [here](https://github.com/luizalabs/hello-teresa) to
make sure that your application is ready for Teresa.

## FAQ

### Config

**Q: How to list the available clusters (aka environments)?**

    $ teresa config view

**Q: How to add/update a cluster?**

    $ teresa config set-cluster <cluster-name> --server <cluster-endpoint>

**Q: How to start using a cluster?**

    $ teresa config use-cluster <cluster-name>
    $ teresa login --user <email>

### App

**Q: How to create an app?**

    $ teresa app create <app-name> --team <team-name>

**Q: How to create an app without a load balancer (a worker for example) ?**

    $ teresa app create <app-name> --team <team-name> --process-type worker

You also have to adjust the `Procfile` to have a corresponding `worker` key.
There's nothing special with the name `worker`, it can be anything different
from `web` with a matching `Procfile` line.

**Q: How to get info about an app?**

    $ teresa app info <app-name>

**Q: How to get the list of apps I have access to?**

    $ teresa app list

**Q: How to get app logs?**

    $ teresa app logs <app-name>

**Q: How to set an environment variable?**

    $ teresa app env-set KEY=VALUE --app <app-name>

**Q: How to unset an environment variable?**

    $ teresa app env-unset KEY --app <app-name>

**Q: How to deploy an app?**

    $ teresa deploy create /path/to/project --app <app-name> --description "version 1.0"

**Q: How to set up Kubernetes health checks?**

Take a look at [here](https://github.com/luizalabs/hello-teresa#teresayaml).

**Q: I need one `teresa.yaml` per process type, how to proceed?**

If a file named `teresa-processtype.yaml` is found it is used instead of
`teresa.yaml`.

**Q: How to drain connections on shutdown?**

You can make the pods wait a fixed amount of seconds (maximum 30) before
receiving the *SIGTERM* signal by adding this lines to `teresa.yaml`:

```yaml
lifecycle:
  preStop:
    drainTimeoutSeconds: 10
```

**Q: What's the deployment strategy?**

Teresa creates a rolling update deployment, which updates a fixed number of
pods at a time. Take a look [here](https://github.com/luizalabs/hello-teresa#rolling-update)
on how to configure the rolling update process.

**Q: How to perform tasks before a new release is deployed?**

There's a special kind of process called **release**, which is executed right
after the build process and before the rolling update. It is useful for tasks
such as sending javascript to a CDN or running database schema migrations. For
example, if you are running a django based application you may configure
automatic migrations by adding this line to the `Procfile`:

    release: python django/manage.py migrate

Note that a failing release will prevent the rolling update from happening, so
you have to keep compatibility with old code.

## Homebrew Teresa Client

Run the following in your command-line:

```sh
$ brew tap luizalabs/teresa-cli
$ brew install teresa
```
