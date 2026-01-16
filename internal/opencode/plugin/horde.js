// Horde OpenCode plugin: hooks SessionStart/Compaction via events.
export const Horde = async ({ $, directory }) => {
  const role = (process.env.GT_ROLE || "").toLowerCase();
  const autonomousRoles = new Set(["raider", "witness", "forge", "shaman"]);
  let didInit = false;

  const run = async (cmd) => {
    try {
      await $`/bin/sh -lc ${cmd}`.cwd(directory);
    } catch (err) {
      console.error(`[horde] ${cmd} failed`, err?.message || err);
    }
  };

  const onSessionCreated = async () => {
    if (didInit) return;
    didInit = true;
    await run("hd rally");
    if (autonomousRoles.has(role)) {
      await run("hd drums check --inject");
    }
    await run("hd signal shaman session-started");
  };

  return {
    event: async ({ event }) => {
      if (event?.type === "session.created") {
        await onSessionCreated();
      }
    },
  };
};
