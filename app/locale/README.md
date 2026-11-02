# Launcher and in-game translations

These JSON files are build inputs for translators. The frontend build bundles its own copy into the launcher, and the Fang build embeds a separate copy into Fang. Neither application loads a shared translation file from the user's computer. There are no translation-file controls in the launcher.

Give each translator the JSON file for their language:

| File | Language |
| --- | --- |
| `de-de.json` | German |
| `fr-fr.json` | French |
| `pl-pl.json` | Polish |
| `ru-ru.json` | Russian |

`en-us.json` is the English reference. German, French, Polish, and Russian include draft machine translations as placeholders for native-speaker review. Review terminology, grammar, and how captions fit in the UI before treating a draft as final. Return the edited JSON files to incorporate revisions into the launcher and Fang on the next normal build.

## Editing a file

Keep `locale` and every key in `messages` unchanged. Review and edit the translated string on the right of each English key:

```json
{
  "locale": "fr-fr",
  "messages": {
    "PLAY": "JOUER",
    "EDIT {0}": "MODIFIER {0}"
  }
}
```

This is an example, not a replacement for the complete file. Translate only the strings on the right. Leave a translation empty to keep its English fallback. Preserve meaningful spaces and punctuation, and save as UTF-8 JSON. Escape quotation marks inside a translation as `\"`; JSON does not allow comments or trailing commas.

Keep numbered placeholders such as `{0}` and `{1}` unchanged. They represent names, counts, versions, or error details; they may be moved to suit the sentence. For example, `PLAY IN {0} · CANCEL` includes the countdown in `{0}`. Keep technical names and commands such as `SporeApp.exe`, `darkspin.toml`, `/bug`, and `/ss dump` intact. Preserve product names such as Darkspinner, Steam, Spore, and GitHub.

The catalog includes launcher navigation, configuration, onboarding, Crogenitor creation and actions, remote connections, server rules, reporting dialogs, accessibility labels, and launcher progress and failure messages. It also includes Dark Spin's in-game options captions, creature editor actions and validation hints, preview buttons and tooltips, the Parts shop tab, and fallback part-category names. Some messages are fragments around emphasized text or player data; retain their punctuation and how they connect to the surrounding sentence.

Config General has independent Launcher Language and In-Game Language dropdowns. Both auto-save to `darkspin.toml`: `launcher.locale` controls launcher text immediately, while `game.locale` controls the next game's language. The launcher offers all bundled translation languages; the game offers languages installed with its assets. When the launcher preference is first created, it preserves the previously shared explicit language choice, or uses the OS-language fallback. Fang selects its embedded translations from the game's existing `-locale:` launch argument, so added in-game captions follow that game's language for the lifetime of the process. A missing translation falls back to English; an unsupported launcher locale also falls back to English.

Player names, server addresses, filesystem paths, imported creature and hero names, raw logs, Lua source, report payloads, and published changelog content stay verbatim. External program and operating-system diagnostics without a catalog entry remain available in their original form.

The game's installed localization and assets remain unchanged. These translations apply only to Dark Spin-owned additions and fallback captions, preserving native localized names when available. Future added captions should use `translate`/`$t` in the launcher or `fang_text`, `fang_text_wide`, and `fang_format_text` in Fang, and add their English source keys to all five JSON files. Use numbered placeholders for formatted native hints; translated strings must never be used as printf formats.
