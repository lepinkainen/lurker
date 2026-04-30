import type { Buffer } from "./app-state";

export type SlashCommand = {
  cmd: string;
  args: string;
  desc: string;
};

type SendCmd = (cmd: Record<string, unknown>) => void;

type SlashCommandHandler = (ctx: { buffer: Buffer; rest: string[]; args: string; sendCmd: SendCmd }) => void;

type SlashCommandEntry = SlashCommand & {
  names: string[];
  aliases?: SlashCommand[];
  handle: SlashCommandHandler;
};

function command(cmd: string, args: string, desc: string, handle: SlashCommandHandler): SlashCommandEntry {
  return { cmd, args, desc, names: [cmd.slice(1)], handle };
}

function alias(
  cmd: string,
  aliasName: string,
  args: string,
  desc: string,
  handle: SlashCommandHandler,
  aliasDesc = `${desc} (alias)`,
): SlashCommandEntry {
  return {
    cmd,
    args,
    desc,
    names: [cmd.slice(1), aliasName],
    aliases: [{ cmd: `/${aliasName}`, args, desc: aliasDesc }],
    handle,
  };
}

function networkCommand(type: string, cmd: string, args: string, desc: string): SlashCommandEntry {
  return command(cmd, args, desc, ({ buffer, args, sendCmd }) => {
    sendCmd({ type, network_id: buffer.network_id, content: args });
  });
}

function networkTargetCommand(type: string, cmd: string, args: string, desc: string): SlashCommandEntry {
  return command(cmd, args, desc, ({ buffer, args, sendCmd }) => {
    sendCmd({ type, network_id: buffer.network_id, target: args });
  });
}

function bufferCommand(type: string, cmd: string, args: string, desc: string): SlashCommandEntry {
  return command(cmd, args, desc, ({ buffer, args, sendCmd }) => {
    sendCmd({ type, buffer_id: buffer.id, content: args });
  });
}

function bufferTargetCommand(type: string, cmd: string, args: string, desc: string): SlashCommandEntry {
  return command(cmd, args, desc, ({ buffer, args, sendCmd }) => {
    sendCmd({ type, buffer_id: buffer.id, target: args });
  });
}

const registry: SlashCommandEntry[] = [
  command("/join", "<channel>", "Join a channel", ({ buffer, args, sendCmd }) => {
    sendCmd({ type: "join", network_id: buffer.network_id, channel: args });
  }),
  bufferCommand("part", "/part", "[reason]", "Leave current channel"),
  command("/msg", "<nick> <text>", "Send private message", ({ buffer, rest, sendCmd }) => {
    const [target, ...msgRest] = rest;
    if (target) {
      sendCmd({ type: "msg", network_id: buffer.network_id, target, content: msgRest.join(" ") });
    }
  }),
  bufferCommand("me", "/me", "<action>", "Send /me action"),
  networkCommand("nick", "/nick", "<newnick>", "Change your nick"),
  bufferCommand("topic", "/topic", "[new topic]", "View or set channel topic"),
  networkTargetCommand("whois", "/whois", "<nick>", "Query user info"),
  command("/invite", "<nick> [channel]", "Invite nick to channel", ({ buffer, rest, sendCmd }) => {
    const [target, channel] = rest;
    sendCmd({ type: "invite", network_id: buffer.network_id, target, channel: channel || buffer.name });
  }),
  command("/kick", "<nick> [reason]", "Kick nick from channel", ({ buffer, rest, sendCmd }) => {
    const [target, ...reason] = rest;
    sendCmd({ type: "kick", buffer_id: buffer.id, target, content: reason.join(" ") });
  }),
  bufferCommand("mode", "/mode", "<modes> [params]", "Set channel modes"),
  networkCommand("raw", "/raw", "<line>", "Send raw IRC line"),
  networkCommand("away", "/away", "[message]", "Set away status"),
  command("/back", "", "Clear away status", ({ buffer, sendCmd }) => {
    sendCmd({ type: "back", network_id: buffer.network_id });
  }),
  networkCommand("quit", "/quit", "[message]", "Disconnect from network"),
  alias(
    "/rejoin",
    "cycle",
    "",
    "Rejoin current channel",
    ({ buffer, sendCmd }) => {
      sendCmd({ type: "rejoin", buffer_id: buffer.id });
    },
    "Rejoin current channel (alias)",
  ),
  command("/notice", "<target> <text>", "Send a NOTICE", ({ buffer, rest, sendCmd }) => {
    const [target, ...msgRest] = rest;
    if (target) {
      sendCmd({ type: "notice", network_id: buffer.network_id, target, content: msgRest.join(" ") });
    }
  }),
  command("/ctcp", "<nick> <cmd> [args]", "Send CTCP request", ({ buffer, rest, sendCmd }) => {
    const [target, ctcpCmd, ...ctcpArgs] = rest;
    if (target && ctcpCmd) {
      sendCmd({
        type: "ctcp",
        network_id: buffer.network_id,
        target,
        content: ctcpCmd + (ctcpArgs.length > 0 ? ` ${ctcpArgs.join(" ")}` : ""),
      });
    }
  }),
  networkTargetCommand("query", "/query", "<nick>", "Open PM buffer without messaging"),
  networkCommand("list", "/list", "[filter]", "List channels on server"),
  bufferTargetCommand("op", "/op", "<nick>", "Give channel op (+o)"),
  bufferTargetCommand("deop", "/deop", "<nick>", "Remove channel op (-o)"),
  bufferTargetCommand("voice", "/voice", "<nick>", "Give voice (+v)"),
  bufferTargetCommand("devoice", "/devoice", "<nick>", "Remove voice (-v)"),
  bufferTargetCommand("ban", "/ban", "<mask>", "Ban mask (+b)"),
  bufferTargetCommand("unban", "/unban", "<mask>", "Unban mask (-b)"),
  command("/banlist", "", "Show channel ban list", ({ buffer, sendCmd }) => {
    sendCmd({ type: "banlist", buffer_id: buffer.id });
  }),
  command("/kickban", "<nick> [reason]", "Kick and ban nick", ({ buffer, rest, sendCmd }) => {
    const [target, ...reason] = rest;
    if (target) {
      sendCmd({ type: "kickban", buffer_id: buffer.id, target, content: reason.join(" ") });
    }
  }),
  networkTargetCommand("ignore", "/ignore", "<nick>", "Ignore nick or mask"),
  networkTargetCommand("unignore", "/unignore", "<nick>", "Remove ignore"),
  command("/ignorelist", "", "List ignored masks", ({ buffer, sendCmd }) => {
    sendCmd({ type: "ignorelist", network_id: buffer.network_id });
  }),
];

const registryByName = new Map(registry.flatMap((entry) => entry.names.map((name) => [name, entry] as const)));
const WHITESPACE_RE = /\s+/u;

export const SLASH_COMMANDS: SlashCommand[] = registry.flatMap(({ cmd, args, desc, aliases = [] }) => [
  { cmd, args, desc },
  ...aliases,
]);

export function matchSlashCommands(query: string): SlashCommand[] {
  return SLASH_COMMANDS.filter((command) => command.cmd.slice(1).startsWith(query));
}

export function handleSlashCommand(text: string, buffer: Buffer, sendCmd: SendCmd): boolean {
  const [cmd, ...rest] = text.slice(1).split(WHITESPACE_RE);
  const entry = registryByName.get((cmd || "").toLowerCase());
  if (!entry) return false;
  entry.handle({ buffer, rest, args: rest.join(" ").trim(), sendCmd });
  return true;
}
