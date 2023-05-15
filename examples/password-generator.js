import { check } from "k6";
import { Cmd } from "k6/x/cmd";

export const options = {
  vus: 1,
  iterations: 1,
};

export default async function () {
  // Produce the command you wish to run
  // using the composable API.
  const cmd = new Cmd("motus")
    .arg("memorable")
    .arg("--words")
    .arg("12")
    .env("NO_COLOR", "true");

  // Execute the command and wait for it to finish.
  const result = await cmd.exec();

  // Check the result of the command.
  check(result, {
    "exit code is 0": (r) => r.exitCode === 0,
    "stdout contains 12 words": (r) => r.stdout.split(" ").length === 12,
  });
}
