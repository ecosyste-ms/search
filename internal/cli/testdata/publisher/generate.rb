require 'digest'
require 'json'
require 'tmpdir'
require 'time'
require 'zlib'

# Run with a packages checkout: ruby generate.rb /path/to/packages
publisher = File.expand_path(ARGV.fetch(0))
require File.join(publisher, 'lib/corpus/export')

record = {
  'package_id' => 1, 'name' => '@example/transfer', 'ecosystem' => 'npm',
  'namespace' => 'example', 'purl' => 'pkg:npm/%40example/transfer', 'status' => 'active',
  'registry' => {'id' => 1, 'name' => 'npmjs.org', 'ecosystem' => 'npm', 'packages_count' => 1},
  'description' => 'Transfer data with an HTTP client', 'repository' => nil,
  'signals' => {'downloads' => 0}, 'activity' => {}, 'provenance' => {}
}

Dir.mktmpdir do |dir|
  source = File.join(dir, 'packages.ndjson.gz')
  assignments = File.join(dir, 'assignments.ndjson.gz')
  Zlib::GzipWriter.open(source) { |gz| gz.mtime = 0; gz.puts(JSON.generate(record)) }
  assignment = {'package_id' => 1, 'project_id' => 'package:1', 'previous_project_id' => 'package:1', 'display_name' => '@example/transfer', 'reason' => 'package'}
  Zlib::GzipWriter.open(assignments) { |gz| gz.mtime = 0; gz.puts(JSON.generate(assignment)) }
  manifest = File.join(dir, 'assignment-manifest.json')
  File.write(manifest, JSON.generate(
    packages: 1, projects: 1, assignments_file: 'assignments.ndjson.gz',
    assignments_sha256: Digest::SHA256.file(assignments).hexdigest,
    source_package_sha256: Digest::SHA256.file(source).hexdigest,
    as_of: '2026-10-31T00:00:00Z'
  ))
  output = File.join(dir, 'replay.tar.gz')
  Corpus::Export.new(output: output, manifest: manifest, source: source).run
  FileUtils.cp(output, File.join(__dir__, 'replay.tar.gz'))

  # Replace database access only; selection, grouping and archive writing use the publisher.
  database = Object.new
  database.define_singleton_method(:catalog) { {'estimated_rows' => 1} }
  database.define_singleton_method(:queries) { 2 }
  database.define_singleton_method(:read) do |sql, **|
    if sql.start_with?('SELECT * FROM registries')
      [record.fetch('registry')]
    elsif sql.start_with?('SELECT id,')
      [{'id' => 1, 'downloads' => 0}]
    else
      raise "Unexpected database query: #{sql}"
    end
  end
  Corpus::Database.define_singleton_method(:new) { database }
  registry_model = Class.new
  registry_model.define_singleton_method(:instantiate) { |row| row }
  Object.const_set(:Registry, registry_model)
  output = File.join(dir, 'database.tar.gz')
  export = Corpus::Export.new(output: output, target: 1)
  export.define_singleton_method(:database_batch) { |_ids| [record] }
  export.run
  FileUtils.cp(output, File.join(__dir__, 'database.tar.gz'))
end

%w[replay.tar.gz database.tar.gz].each { |name| File.chmod(0o644, File.join(__dir__, name)) }

files = %w[export selection projects identity database].to_h do |name|
  path = "lib/corpus/#{name}.rb"
  [path, Digest::SHA256.file(File.join(publisher, path)).hexdigest]
end
File.write(File.join(__dir__, 'provenance.json'), JSON.pretty_generate(
  publisher: 'https://github.com/ecosyste-ms/packages/pull/1938',
  source_files: files,
  fixtures: %w[replay.tar.gz database.tar.gz].to_h { |name| [name, Digest::SHA256.file(File.join(__dir__, name)).hexdigest] },
  generation: 'generate.rb uses Corpus::Export; database access is replaced with one synthetic package. No database or Rails application is loaded.'
) + "\n")
