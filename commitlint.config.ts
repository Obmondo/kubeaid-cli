import type { UserConfig } from "@commitlint/types";

const configuration: UserConfig = {
	extends: ["@commitlint/config-conventional"],

	rules: {
		"header-max-length": [2, "always", 200],
	},
};

export default configuration;
