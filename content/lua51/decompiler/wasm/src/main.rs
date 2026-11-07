use std::io::{self, Read, Write};
use unluac::decompile::{decompile, DecompileDialect, DecompileOptions, GenerateMode, GeneratedChunkKind};
use unluac::parser::ParseMode;

fn convert() -> Result<(), Box<dyn std::error::Error>> {
    let mut payload = Vec::new();
    io::stdin().read_to_end(&mut payload)?;
    let mut options = DecompileOptions::default();
    options.dialect = DecompileDialect::Lua51;
    options.parse.mode = ParseMode::Strict;
    options.generate.mode = GenerateMode::Strict;
    let result = decompile(&payload, options)?;
    let generated = result.state.generated.ok_or("decompiler produced no source")?;
    if generated.kind != GeneratedChunkKind::Source {
        return Err("decompiler produced diagnostic pseudocode".into());
    }
    io::stdout().write_all(generated.source.as_bytes())?;
    Ok(())
}

fn main() {
    if let Err(error) = convert() {
        eprintln!("{error}");
        std::process::exit(1);
    }
}
