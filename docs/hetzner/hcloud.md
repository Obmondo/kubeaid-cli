# Setting up KubeAid on Hcloud

- Login to Hetzner.

- Click on the `grid icon` on the top right corner and select `Cloud`. This will take you to the HCloud console.

- Click on the left menu bar, select `Projects` and create an `HCloud project`.

- Generate an HCloud API token following [this](https://docs.hetzner.com/cloud/api/getting-started/generating-api-token).

- Generate an SSH KeyPair and add it in the `SSH keys` section of the `Security` tab by following [this](<https://docs.hetzner.com/cloud/servers/getting-started/connecting-to-the-server/#cli-warning>].

- For GitHub PAT generation instructions, see [here](../github.md). This PAT token will be used as your password in secrets.yaml.

- Then follow the instructions from [here](../hetzner.md) to continue. Once you run the config generate command for hcloud flavor, please change the given parameters according to your needs and then proceed.
