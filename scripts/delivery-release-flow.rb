# frozen_string_literal: true

require 'digest'
require 'fileutils'
require 'json'
require 'open3'
require 'optparse'
require 'psych'
require 'tempfile'
require 'time'
require 'uri'

module DeliveryReleaseFlow
  class Failure < StandardError; end

  ENVS = %w[kind kind-small staging-like prod-like].freeze
  CORE = 'ontoaxis-core'
  WEB = 'ontoaxis-web'
  DIGEST = /\Asha256:(?!0{64}\z)[0-9a-f]{64}\z/.freeze
  REVISION = /\A(?:[0-9a-f]{40}|[0-9a-f]{64})\z/.freeze
  NAME = /\A[a-z][a-z0-9-]*\z/.freeze
  REPOSITORY = /\A(?:[a-z0-9][a-z0-9.-]*(?::[0-9]+)?\/)?[a-z0-9]+(?:[._-][a-z0-9]+)*(?:\/[a-z0-9]+(?:[._-][a-z0-9]+)*)*\z/.freeze
  SOURCE_CHECKS = %w[edge-port-forward core-healthz core-readyz read-only-api web-root search-path gitops running-images probes-pdb-secret idp-discovery].freeze
  CHARTS = %w[ontoaxis-app ontoaxis-platform ontoaxis-data ontoaxis-observability ontoaxis-jobs].freeze
  ALIASES = { WEB => 'web', 'ontoaxis-search-worker' => 'ontoaxis-search' }.freeze
  LOCAL_GIT = 'http://ontoaxis-git.argocd.svc.cluster.local:8080/ontoaxis.git'.freeze
  SHA1 = /\A[0-9a-f]{40}\z/.freeze
  SCHEMA_QUERY = 'SELECT max(version_id) FROM (SELECT DISTINCT ON (version_id) version_id, is_applied FROM public.goose_db_version ORDER BY version_id, id DESC) AS migration_versions WHERE is_applied'.freeze

  class StrictJSON < Hash
    def []=(key, value)
      raise Failure, 'JSON 含重复字段' if key?(key)

      super
    end
  end

  module_function

  def check(condition, message)
    raise Failure, message unless condition
  end

  def capture(label, *command)
    stdout, _stderr, status = Open3.capture3(*command, stdin_data: '')
    check(status.success?, "#{label}失败（退出码 #{status.exitstatus || '信号'}）")
    stdout
  rescue Errno::ENOENT
    raise Failure, "缺少命令 #{command.first}"
  end

  def file(path)
    check(File.file?(path), "文件不存在：#{path}")
    File.binread(path)
  end

  def unique_yaml(node)
    case node
    when Psych::Nodes::Mapping
      keys = node.children.each_slice(2).map do |key, value|
        check(key.is_a?(Psych::Nodes::Scalar) && key.value != '<<', '禁止复杂 YAML 键和 merge')
        unique_yaml(value)
        key.value
      end
      check(keys.uniq == keys, 'YAML 含重复字段')
    when Psych::Nodes::Alias
      raise Failure, '禁止 YAML 别名'
    else
      (node.children || []).each { |child| unique_yaml(child) }
    end
  end

  def yaml(text, label)
    tree = Psych.parse_stream(text)
    check(tree.children.size == 1, "#{label}必须是单份 YAML")
    unique_yaml(tree)
    data = Psych.safe_load(text)
    check(data.is_a?(Hash), "#{label}必须是映射")
    data
  rescue Psych::Exception, ArgumentError
    raise Failure, "#{label}的 YAML 无效"
  end

  def json(text, label)
    data = JSON.parse(text, object_class: StrictJSON)
    check(data.is_a?(Hash), "#{label}必须是 JSON 对象")
    data
  rescue JSON::ParserError
    raise Failure, "#{label}的 JSON 无效"
  end

  def timestamp?(value)
    value.is_a?(String) && value.match?(/\A\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?Z\z/) && Time.iso8601(value) > Time.utc(1970)
  rescue ArgumentError
    false
  end

  def schema_version(value)
    return value if value.is_a?(Integer) && value.positive?
    return value.to_i if value.is_a?(String) && value.match?(/\A[1-9]\d*\z/)

    raise Failure, '缺少有效 schema_version；不得推断或覆盖数据库迁移版本'
  end

  def release(text, env)
    data = yaml(text, '发布清单')
    check(data['environment'] == env, '发布清单环境不匹配')
    schema_version(data['schema_version'])
    check(timestamp?(data['created_at']), '发布清单缺少有效 created_at')
    check(data['components'].is_a?(Array) && !data['components'].empty?, '发布清单没有镜像组件')
    components = {}
    data['components'].each do |entry|
      check(entry.is_a?(Hash), '发布组件格式无效')
      name = entry['component']
      check(NAME.match?(name.to_s) && !components.key?(name), '组件名无效或重复')
      check(entry['repository'].is_a?(String) && REPOSITORY.match?(entry['repository']), "组件 #{name} 的仓库无效")
      check(DIGEST.match?(entry['digest'].to_s), "组件 #{name} 缺少真实镜像摘要")
      commit = entry['commit']
      check(commit.is_a?(String) && (REVISION.match?(commit) || commit.match?(/\Aupstream:[A-Za-z0-9_.-]+\z/)), "组件 #{name} 缺少提交或上游版本")
      check(entry['chartVersion'].is_a?(String) && entry['chartVersion'].match?(/\A[A-Za-z0-9.+-]*\z/), "组件 #{name} 缺少 chartVersion")
      components[name] = entry.slice('component', 'repository', 'digest', 'commit', 'chartVersion')
    end
    check(components.key?(CORE) && components.key?(WEB), '发布清单缺少核心或 Web 镜像')
    [schema_version(data['schema_version']), components]
  end

  def image(entry)
    "#{entry.fetch('repository')}@#{entry.fetch('digest')}"
  end

  def smoke(path, env, components, profile, context: nil)
    data = json(file(path), '烟测报告')
    check(data['environment'] == env && (!data.key?('env') || data['env'] == env), '烟测报告环境与发布清单不匹配')
    allowed_profiles = profile == 'full' ? %w[full] : %w[readiness full]
    check(allowed_profiles.include?(data['profile']), '烟测报告不是所需完整级别')
    check(!context || data['context'] == context, '烟测报告集群上下文不匹配')
    check(data['passed'] == true && (!data.key?('overall') || data['overall'] == 'passed'), '烟测未通过（要求原生布尔值 true）')
    checks = data['checks']
    required = SOURCE_CHECKS + (data['profile'] == 'full' ? %w[backup-marker rollback-marker] : [])
    check(checks.is_a?(Array) && checks.all? { |item| item.is_a?(Hash) && item['name'].is_a?(String) && item['ok'] == true }, '烟测报告含失败或无效检查项')
    names = checks.map { |item| item.fetch('name') }
    check(names.uniq == names && (required - names).empty?, '烟测报告检查项缺失或重复')
    check(data['digest'] == components.fetch(CORE).fetch('digest'), '烟测核心摘要与发布清单不匹配')
    check(data['images'] == components.transform_values { |entry| image(entry) }, '烟测运行镜像与发布清单不完全匹配')
  end

  def options(mode)
    args = { timeout: 300 }
    parser = OptionParser.new do |p|
      p.banner = if mode == 'promote'
                   '用法: delivery-promote.sh --from ENV --to ENV [--target-release FILE] [--commit]'
                 else
                   '用法: delivery-rollback.sh --env ENV --previous FILE [--commit --context CONTEXT] 或 --env ENV --context CONTEXT --sync SHA'
                 end
      p.separator '不传 --commit/--sync 时只检查计划；任何模式都不会推送。'
      p.on('--commit', '明确批准生成本地 Git 提交') { args[:commit] = true }
      p.on('--no-commit', '仅检查计划（默认）') { args[:no_commit] = true }
      p.on('--confirm', '明确确认 prod-like 操作') { args[:confirm] = true }
      p.on('--help', '显示帮助') { puts p; exit 0 }
      if mode == 'promote'
        p.on('--from ENV') { |v| args[:from] = v }
        p.on('--to ENV') { |v| args[:env] = v }
        p.on('--source-smoke FILE') { |v| args[:source_smoke] = v }
        p.on('--release FILE', '--source-release FILE', '来源发布清单') { |v| args[:release] = v }
        p.on('--target-release FILE', '同一构建对应的目标环境镜像清单（Web 须按目标环境构建）') { |v| args[:target_release] = v }
        p.on('--digest DIGEST', '预期的源核心镜像摘要') { |v| args[:digest] = v }
        p.on('--oci-proof FILE', '离线 JSON：镜像完整引用 → OCI manifest 文件（相对 JSON 所在目录）') { |v| args[:proof] = v }
      else
        p.on('--env ENV') { |v| args[:env] = v }
        p.on('--previous FILE', '--release FILE', '--to FILE') { |v| args[:release] = v }
        p.on('--previous-smoke FILE', '回滚目标发布的历史 readiness 或 full 烟测报告') { |v| args[:source_smoke] = v }
        p.on('--context CONTEXT', '显式指定 Kubernetes 上下文') { |v| args[:context] = v }
        p.on('--sync SHA', '只同步经审阅、已发布到 GitOps 源的完整回滚提交') { |v| args[:sync] = v }
        p.on('--gitops-root-revision SHA', 'kind 同步：已人工发布并指向回滚提交的固定根 SHA') { |v| args[:root_revision] = v }
        p.on('--timeout SECONDS', Integer) { |v| args[:timeout] = v }
      end
    end
    parser.parse!
    check(ARGV.empty?, '不支持位置参数；使用 --help 查看用法')
    check(ENVS.include?(args[:env]), '必须指定支持的目标环境')
    check(!args[:commit] || !args[:no_commit], '--commit 与 --no-commit 互斥')
    check(!args[:sync] || (!args[:commit] && !args[:no_commit] && !args[:release] && !args[:source_smoke]), '--sync 不得与回滚计划或提交参数同时使用')
    check(args[:env] != 'prod-like' || args[:confirm], 'prod-like 必须显式传入 --confirm')
    if mode == 'promote'
      check(ENVS.include?(args[:from]) && args[:from] != args[:env], '必须指定不同的受支持来源环境')
      check(args[:target_release], '跨环境晋升必须传入 --target-release（Web 的 OIDC 按目标环境构建）')
    else
      check(args[:sync] || (args[:release] && args[:source_smoke]), '必须显式指定 --previous 和 --previous-smoke')
      check(!args[:sync] || REVISION.match?(args[:sync]), '--sync 需要完整提交 SHA')
      check(args[:timeout].between?(1, 3600), '--timeout 必须是 1–3600 秒')
      check(!(args[:commit] || args[:sync]) || (args[:context] && !args[:context].empty?), '真实回滚操作必须显式指定 --context')
      check(!%w[kind-small prod-like].include?(args[:env]) || !(args[:commit] || args[:sync]),
            'kind-small 没有独立回滚证据，prod-like 仅支持本地渲染；拒绝连接集群')
      if args[:sync] && args[:env] == 'kind'
        check(args[:root_revision] && SHA1.match?(args[:root_revision]) && SHA1.match?(args[:sync]), 'kind 同步要求 --gitops-root-revision 与 --sync 都是完整 SHA-1')
      else
        check(!args.key?(:root_revision), '--gitops-root-revision 仅用于 kind --sync')
      end
    end
    args
  end

  def inputs(args, mode)
    env = args.fetch(:env)
    if mode == 'rollback'
      version, images = release(file(args.fetch(:release)), env)
      smoke(args.fetch(:source_smoke), env, images, 'readiness')
      return [version, images]
    end

    from = args.fetch(:from)
    source_version, source = release(file(args.fetch(:release, "deploy/releases/#{from}.yaml")), from)
    smoke(args.fetch(:source_smoke, "dist/delivery/smoke-#{from}.json"), from, source, 'full')
    check(!args[:digest] || args[:digest] == source.fetch(CORE).fetch('digest'), '指定核心摘要与来源发布不匹配')
    target_version, target = release(file(args.fetch(:target_release)), env)
    check(source_version == target_version && source.keys.sort == target.keys.sort, '来源和目标的组件集合或 schema_version 不匹配')
    source.each do |name, entry|
      candidate = target.fetch(name)
      check(entry['commit'] == candidate['commit'] && entry['chartVersion'] == candidate['chartVersion'], "组件 #{name} 的构建来源不匹配")
      if name == WEB
        check(entry['digest'] != candidate['digest'], '禁止跨环境复用构建期 OIDC 不同的 Web 镜像')
      else
        check(entry['digest'] == candidate['digest'], "组件 #{name} 的目标镜像不是已验证的源摘要")
      end
      registry = candidate.fetch('repository').split('/').first
      if %w[staging-like prod-like].include?(env)
        check(!registry.match?(/\A(?:localhost|127\.[0-9.]+|0\.0\.0\.0)(?::\d+)?\z/), "组件 #{name} 不能晋升本地镜像仓库地址")
      end
    end
    [target_version, target]
  end

  def verify_registry(components, proof_path = nil)
    proof = json(file(proof_path), 'OCI 证明') if proof_path
    components.each_value do |entry|
      ref = image(entry)
      bytes = if proof
                path = proof[ref]
                check(path.is_a?(String) && !path.empty?, "OCI 证明中缺少组件 #{entry.fetch('component')}")
                file(File.expand_path(path, File.dirname(proof_path)))
              else
                capture("镜像仓库检查（#{entry.fetch('component')}）", 'docker', 'buildx', 'imagetools', 'inspect', '--raw', ref)
              end
      # Some CLI writers append a presentation newline; hash raw bytes first.
      actual = [bytes, bytes.sub(/\r?\n\z/, '')].map { |value| "sha256:#{Digest::SHA256.hexdigest(value)}" }
      manifest = json(bytes, '镜像仓库清单')
      check(actual.include?(entry['digest']) && manifest['schemaVersion'] == 2 &&
            ((manifest['config'].is_a?(Hash) && manifest['layers'].is_a?(Array)) ||
             (manifest['manifests'].is_a?(Array) && !manifest['manifests'].empty?)), "组件 #{entry.fetch('component')} 的镜像仓库清单与摘要不符")
    end
  end

  def repo_paths(env)
    ["deploy/values/#{env}.yaml", "deploy/releases/#{env}.yaml"]
  end

  def map_nodes(node)
    return {} unless node.is_a?(Psych::Nodes::Mapping)

    node.children.each_slice(2).to_h { |key, value| [key.value, [key, value]] }
  end

  def patch_values(text, old, expected, components)
    root = Psych.parse(text).root
    lines = text.lines
    edits = []
    # Follow parsed keys, not position or repository text. Insert new mappings
    # at their owner's boundary and preserve unrelated lines and comments.
    add = lambda do |node, key, depth, mapping|
      body = Psych.dump(mapping).lines.drop(1).map { |line| (' ' * depth) + line }.join
      if node.is_a?(Psych::Nodes::Mapping) && node.style == Psych::Nodes::Mapping::BLOCK
        edits << [node.end_line, depth, nil, body]
      else
        check(key && (node.children || []).empty? && node.start_line == node.end_line && key.start_line == node.start_line,
              '请先将目标 values 调整为块式 YAML 映射')
        line = lines.fetch(key.start_line)
        prefix = line[0...node.start_column].rstrip
        suffix = line[node.end_column..-1]
        edits << [key.start_line + 1, depth, prefix + (suffix.start_with?('#') ? ' ' : '') + suffix, body]
      end
    end
    component_key, component_map = map_nodes(root)['components']
    if component_map.nil?
      add.call(root, nil, 0, 'components' => expected.fetch('components'))
    elsif old.fetch('components').empty?
      add.call(component_map, component_key, component_key.start_column + 2, expected.fetch('components'))
    else
      by_name = map_nodes(component_map)
      additions = {}
      components.each do |name, entry|
        key = ALIASES.fetch(name, name)
        pins = entry.slice('repository', 'digest')
        field, node = by_name[key]
        unless node
          additions[key] = { 'image' => pins }
          next
        end
        image_key, image_node = map_nodes(node)['image']
        if image_node && node.is_a?(Psych::Nodes::Mapping)
          image_fields = map_nodes(image_node)
          missing = pins.reject { |pin, _| image_fields.key?(pin) }
          add.call(image_node, image_key, image_key.start_column + 2, missing) unless missing.empty?
          pins.each do |pin, value|
            original_key, scalar = image_fields[pin]
            next unless scalar && old.dig('components', key, 'image', pin) != value

            check(scalar.is_a?(Psych::Nodes::Scalar) && scalar.start_line == scalar.end_line && original_key.start_line == scalar.start_line,
                  'values 中的镜像键值必须在同一行')
            line = lines.fetch(scalar.start_line)
            edits << [scalar.start_line + 1, nil, line[0...scalar.start_column] + value + line[scalar.end_column..-1], nil]
          end
        else
          add.call(node, field, field.start_column + 2, 'image' => pins)
        end
      end
      add.call(component_map, component_key, component_key.start_column + 2, additions) unless additions.empty?
    end
    edits.each { |row, _, replacement, _| lines[row - 1] = replacement if replacement }
    edits.select { |_, _, _, body| body }.sort_by { |row, depth, _, _| [-row, depth] }.each do |row, _, _, body|
      lines[row - 1] += "\n" if row.positive? && !lines[row - 1].end_with?("\n")
      lines.insert(row, body)
    end
    patched = lines.join
    check(yaml(patched, '更新后的环境 values') == expected, 'values 更新包含意外变更；未写入任何文件')
    patched
  end

  def versions(env, version, components)
    values_path, release_path = repo_paths(env)
    source_text = file(values_path)
    previous_values = yaml(source_text, '环境 values')
    values = yaml(source_text, '环境 values')
    check(values.dig('global', 'environment') == env, 'values 环境与目标不匹配')
    values['components'] ||= {}
    check(values['components'].is_a?(Hash), 'values.components 不是映射')
    components.each do |name, entry|
      key = ALIASES.fetch(name, name)
      values['components'][key] ||= {}
      component = values['components'][key]
      check(component.is_a?(Hash) && (!component.key?('image') || component['image'].is_a?(Hash)), "组件 #{name} 的 values 格式无效")
      component['image'] ||= {}
      component['image'].merge!(entry.slice('repository', 'digest'))
    end
    old = File.exist?(release_path) ? yaml(file(release_path), '当前发布清单') : {}
    check(old.empty? || old['environment'] == env, '当前发布清单环境标签不匹配')
    # Never copy arbitrary metadata or secret values from an incoming manifest.
    outgoing = old.slice('chartVersion')
    outgoing.merge!('environment' => env, 'created_at' => Time.now.utc.iso8601,
                    'operator' => 'delivery-release-flow', 'schema_version' => version, 'components' => components.values)
    check(components.keys.map { |name| ALIASES.fetch(name, name) }.uniq.size == components.size, '多个镜像冲突地映射到同一 values 组件')
    { values_path => patch_values(source_text, previous_values, values, components), release_path => Psych.dump(outgoing) }
  end

  def matching_charts(env, values_path, components)
    expected = components.transform_values { |entry| image(entry) }
    result = {}
    CHARTS.each do |chart|
      args = ['helm', 'template', "oa-#{chart.delete_prefix('ontoaxis-')}", "deploy/helm/#{chart}"]
      args += %w[-f deploy/values/common.yaml] if File.file?('deploy/values/common.yaml')
      rendered = capture("#{chart}/#{env} 的 Helm 渲染", *args, '-f', values_path)
      result[chart] = Psych.parse_stream(rendered).children.flat_map do |document|
        stream = Psych::Nodes::Stream.new
        stream.children << document
        object = Psych.safe_load(stream.to_yaml)
        next [] unless object.is_a?(Hash)

        template = object.dig('spec', 'template') || object.dig('spec', 'jobTemplate', 'spec', 'template')
        next [] unless template.is_a?(Hash) && template['spec'].is_a?(Hash)

        label = template.dig('metadata', 'labels', 'ontoaxis.io/component')
        containers = Array(template['spec']['containers'])
        expected.keys.select do |name|
          key = ALIASES.fetch(name, name)
          workload_env = object.dig('metadata', 'labels', 'ontoaxis.io/environment') || template.dig('metadata', 'labels', 'ontoaxis.io/environment')
          label == key && workload_env == env && containers.any? { |c| c.is_a?(Hash) && c['name'] == key && c['image'] == expected[name] }
        end
      end
    end
    missing = expected.keys - result.values.flatten
    check(missing.empty?, "发布镜像未进入对应 Helm 工作负载：#{missing.join(', ')}")
    result.reject { |_chart, used| used.empty? }.keys
  rescue Psych::Exception
    raise Failure, 'Helm 输出不是有效 YAML'
  end

  def validate_render(env, values_path, components)
    matching_charts(env, values_path, components)
    check(system('bash', 'scripts/delivery-render-check.sh'), '交付渲染门禁失败')
  end

  def atomic_write(path, content)
    check(!File.symlink?(path), "拒绝写入符号链接：#{path}")
    Tempfile.create('.delivery-release-', File.dirname(path)) do |temporary|
      temporary.binmode
      temporary.write(content)
      temporary.flush
      temporary.chmod(File.exist?(path) ? File.stat(path).mode & 0o777 : 0o644)
      File.rename(temporary.path, path)
    end
  end

  def commit_release(mode, env, candidate, components)
    paths = candidate.keys
    head = capture('读取当前 Git HEAD', 'git', 'rev-parse', 'HEAD').strip
    check(capture('检查目标文件状态', 'git', 'status', '--porcelain', '--untracked-files=all', '--', *paths).empty?, '目标 values 或 manifest 有用户改动，拒绝覆盖')
    check(capture('检查合并状态', 'git', 'ls-files', '-u').empty?, 'Git 正在处理未合并文件')
    originals = paths.to_h do |path|
      check(!File.symlink?(path) && File.realpath(File.dirname(path)) == File.expand_path(File.dirname(path)), '拒绝非真实发布目录')
      [path, File.exist?(path) ? file(path) : nil]
    end
    changed = false
    committed = false
    begin
      candidate.each do |path, content|
        atomic_write(path, content)
        changed = true
      end
      validate_render(env, paths.first, components)
      check(capture('确认 Git HEAD 未变化', 'git', 'rev-parse', 'HEAD').strip == head, '渲染期间 Git HEAD 变化')
      check(candidate.all? { |path, content| file(path) == content }, '渲染期间发布文件发生意外改动')
      # git commit --only excludes unrelated staged changes. A brand-new release
      # path needs an intent-to-add entry; remove only that entry on failure.
      new_paths = originals.select { |_path, original| original.nil? }.keys
      capture('登记新发布清单', 'git', 'add', '-N', '--', *new_paths) unless new_paths.empty?
      capture('创建本地发布提交', 'git', 'commit', '--only', '-m', "delivery: #{mode} #{env}", '--', *paths)
      committed = true
      revision = capture('读取本地发布提交', 'git', 'rev-parse', 'HEAD').strip
      check(REVISION.match?(revision) && revision != head, '未生成新的本地发布提交')
      files = capture('校验本地提交范围', 'git', 'diff-tree', '--no-commit-id', '--name-only', '-r', revision).lines.map(&:strip)
      check((files - paths).empty? && files.include?(paths.last), '本地提交与授权路径不一致，请检查提交后再继续')
      check(candidate.all? { |path, content| capture('校验提交中的发布文件', 'git', 'show', "#{revision}:#{path}") == content }, '本地提交内容与渲染的文件不一致')
      revision
    ensure
      if changed && !committed
        new_paths&.each { |path| capture('清理新清单暂存条目', 'git', 'reset', '--quiet', 'HEAD', '--', path) }
        paths.each do |path|
          next unless File.file?(path) && file(path) == candidate.fetch(path)

          originals[path] ? atomic_write(path, originals[path]) : File.unlink(path)
        end
      end
    end
  end

  def kubectl(context, *args)
    capture('Kubernetes 操作', 'kubectl', '--context', context, '--request-timeout=15s', *args)
  end

  def running_schema(context, expected, components)
    postgres = components['postgres']
    check(postgres, '回滚发布清单缺少 PostgreSQL 镜像，无法验证数据库不会被替换')
    deployment = json(kubectl(context, '-n', 'oa-data', 'get', 'deployment', 'postgres', '-o', 'json'), '运行中的 PostgreSQL Deployment')
    containers = deployment.dig('spec', 'template', 'spec', 'containers')
    images = Array(containers).select { |container| container.is_a?(Hash) && container['name'] == 'postgres' }.map { |container| container['image'] }
    check(images == [image(postgres)], '拒绝在同 schema 镜像回滚中替换 PostgreSQL 容器；数据库变更请走单独评审的恢复流程')
    read = "export PGPASSWORD=\"${POSTGRES_PASSWORD:?}\"; PGOPTIONS='-c statement_timeout=5000 -c default_transaction_read_only=on' " \
           "exec psql --no-psqlrc -v ON_ERROR_STOP=1 -Atq -U \"${POSTGRES_USER:?}\" -d \"${POSTGRES_DB:?}\" -c '#{SCHEMA_QUERY}'"
    actual = schema_version(kubectl(context, '-n', 'oa-data', 'exec', 'deployment/postgres', '-c', 'postgres', '--', 'sh', '-ec', read).strip)
    check(actual == expected, "拒绝跨 schema 镜像回滚（运行 #{actual}，目标 #{expected}）：若目标更旧，须先停止写入、经授权恢复备份到独立实例、部署旧镜像并校验；不执行 down migration")
  end

  def argocd(context, *args)
    capture('ArgoCD 操作', 'argocd', '--core', '--kube-context', context, '--app-namespace', 'argocd', *args)
  end

  def applications(context, env)
    result = json(kubectl(context, '-n', 'argocd', 'get', 'applications.argoproj.io', '-l', "ontoaxis.io/environment=#{env}", '-o', 'json'), 'ArgoCD Applications')
    items = result['items']
    check(items.is_a?(Array) && !items.empty?, '未找到环境对应的 ArgoCD Applications')
    items.to_h do |app|
      name = app.dig('metadata', 'name')
      check(NAME.match?(name.to_s) && app.dig('metadata', 'labels', 'ontoaxis.io/environment') == env, 'ArgoCD 应用身份不匹配')
      [name, app]
    end.tap { |by_name| check(by_name.size == items.size, 'ArgoCD 应用名重复') }
  end

  def git_source(repo)
    url = URI.parse(repo.to_s)
    check(%w[https ssh].include?(url.scheme) && url.host && url.path && !url.query && !url.fragment && !url.password &&
          (url.scheme == 'ssh' || !url.userinfo), 'GitOps 仓库需使用不含内嵌凭据的 HTTPS 或 ssh:// URL')
    repo.sub(%r{/\z}, '').sub(/\.git\z/, '')
  rescue URI::Error
    raise Failure, 'GitOps 仓库 URL 无效'
  end

  def local_context(context)
    check(ENV['KUBECONFIG'] && File.file?(ENV['KUBECONFIG']), 'kind 回滚需要显式指定独立的 KUBECONFIG 文件')
    check(context.match?(/\A(?:kind|k3d)-[A-Za-z0-9-]+\z/), 'kind 回滚只允许 kind-* 或 k3d-* 上下文')
    current = capture('核验本地 kubeconfig 上下文', 'kubectl', 'config', 'current-context').strip
    server = kubectl(context, 'config', 'view', '--minify', '-o', 'jsonpath={.clusters[0].cluster.server}').strip
    check(current == context && server.match?(%r{\Ahttps://(?:127\.0\.0\.1|localhost):[0-9]+\z}), 'kind 回滚要求同一当前上下文及回环 Kubernetes API')
  end

  def local_gitops(context, revision, root_revision)
    check(system({ 'GITOPS_CHECKOUT' => Dir.pwd, 'GITOPS_ROOT_REVISION' => root_revision,
                   'GITOPS_CONTENT_REVISION' => revision, 'ROOT_APP_ENV' => 'kind' },
                 'bash', 'deploy/kind/bootstrap.sh', '--check-source'), '本地 GitOps 根与内容提交预检失败')
    service = json(kubectl(context, '-n', 'argocd', 'get', 'service', 'ontoaxis-git', '-o', 'json'), '本地 GitOps Service')
    spec = service['spec'] || {}
    ports = spec['ports']
    check(spec['type'] == 'ClusterIP' && !['', 'None'].include?(spec['clusterIP'].to_s) && Array(spec['externalIPs']).empty? &&
          ports.is_a?(Array) && ports.size == 1 && ports.first.is_a?(Hash) && ports.first['port'] == 8080 &&
          ports.first.fetch('protocol', 'TCP') == 'TCP', '本地 GitOps Service 必须只开放集群内 TCP 8080')
    root = applications(context, 'kind')['ontoaxis-root']
    check(root && root.dig('spec', 'project') == 'ontoaxis' && root.dig('spec', 'destination', 'server') == 'https://kubernetes.default.svc' &&
          !root.dig('spec', 'sources') && root.dig('spec', 'source') == {
            'repoURL' => LOCAL_GIT, 'targetRevision' => root_revision, 'path' => 'deploy/argocd/apps', 'directory' => { 'recurse' => false }
          } && root.dig('status', 'sync', 'status') == 'Synced' && root.dig('status', 'sync', 'revision') == root_revision,
          '运行中的根 Application 未同步人工发布的固定根 SHA；不会修改或发布 GitOps 源')
  end

  def wait_local(args, attempt, timeout)
    check(system({ 'GITOPS_ROOT_REVISION' => args.fetch(:root_revision), 'GITOPS_CONTENT_REVISION' => args.fetch(:sync),
                   'ROOT_APP_ENV' => 'kind', 'VERIFY_CONTEXT' => args.fetch(:context), 'VERIFY_TIMEOUT' => timeout.to_s,
                   'VERIFY_EVIDENCE_DIR' => attempt, 'VERIFY_SMOKE_PROFILE' => 'readiness' },
                 'bash', 'deploy/kind/verify.sh', '--wait-only'), '本地 ArgoCD 应用未全部 Synced/Healthy 并匹配固定根与内容提交')
  end

  def sync_targets(context, env, charts, revision)
    apps = applications(context, env)
    origin = env == 'kind' ? LOCAL_GIT : git_source(capture('读取审核后的 Git origin', 'git', 'remote', 'get-url', 'origin').strip)
    branches = []
    targets = charts.to_h do |chart|
      matches = apps.values.select do |app|
        sources = app.dig('spec', 'sources') || [app.dig('spec', 'source')]
        Array(sources).any? { |source| source.is_a?(Hash) && source['path'] == "deploy/helm/#{chart}" }
      end
      check(matches.size == 1, "#{chart} 缺少唯一的目标环境 ArgoCD 应用")
      app = matches.first
      sources = app.dig('spec', 'sources') || [app.dig('spec', 'source')]
      expected_path = "deploy/helm/#{chart}"
      check(sources.is_a?(Array) && sources.size.between?(1, 2) && sources.all? { |source| source.is_a?(Hash) }, "#{chart} 包含未经审核的 GitOps 源")
      chart_sources = sources.select { |source| source['path'] == expected_path }
      check(chart_sources.size == 1, "#{chart} 缺少唯一的 Helm GitOps 源")
      chart_source = chart_sources.first
      check((chart_source.keys - %w[repoURL targetRevision path helm]).empty?, "#{chart} 的 Helm GitOps 源含额外覆盖配置")
      helm = chart_source['helm']
      check(helm.is_a?(Hash) && (helm.keys - %w[valueFiles releaseName]).empty?, "#{chart} 的 Helm 覆盖配置可能绕过 release 文件")
      check(app.dig('spec', 'project') == 'ontoaxis' && app.dig('spec', 'destination', 'server') == 'https://kubernetes.default.svc',
            "#{chart} 的 GitOps 项目或目标集群不匹配")
      value_path = '../../values'
      if sources.size == 2
        value_source = (sources - chart_sources).first
        check(value_source && (value_source.keys - %w[repoURL targetRevision ref]).empty? && NAME.match?(value_source['ref'].to_s),
              "#{chart} 的 values GitOps 源含额外资源或配置")
        value_path = "$#{value_source['ref']}/deploy/values"
      end

      wanted = "#{value_path}/#{env}.yaml"
      check([[wanted], ["#{value_path}/common.yaml", wanted]].include?(helm['valueFiles']), "#{chart} 没有按顺序读取受审核的目标环境 GitOps values")
      sources.each do |source|
        branch = source['targetRevision']
        if env == 'kind'
          check(source['repoURL'] == origin && branch == revision, "#{chart} 的运行源未指向人工发布的本地回滚 SHA")
        else
          check(git_source(source['repoURL']) == origin, "#{chart} 的 GitOps 源与本地 origin 不一致")
          check(branch.is_a?(String) && branch.match?(/\A(?:refs\/heads\/)?[a-zA-Z0-9][a-zA-Z0-9._\/-]*\z/), 'GitOps 应用必须指向显式 Git 分支')
          branches << (branch.start_with?('refs/heads/') ? branch : "refs/heads/#{branch}")
        end
      end
      [app.dig('metadata', 'name'), sources.size]
    end
    branches.uniq.each do |branch|
      ref = capture('核验回滚提交已由人工发布到 GitOps 分支', 'git', 'ls-remote', '--exit-code', 'origin', branch).strip
      check(ref == "#{revision}\t#{branch}", 'GitOps 分支未指向所选回滚提交；请人工审阅并发布后重试，不会自动推送')
    end
    targets
  end

  def wait_for_revision(context, env, targets, revision, timeout)
    deadline = Process.clock_gettime(Process::CLOCK_MONOTONIC) + timeout
    loop do
      apps = applications(context, env)
      return if targets.all? do |name, count|
        status = apps[name] && apps[name]['status']
        next false unless status.is_a?(Hash)

        sync = status['sync'] || {}
        revisions = count == 1 ? Array(sync['revision'] || sync['revisions']) : Array(sync['revisions'])
        sync['status'] == 'Synced' && status.dig('health', 'status') == 'Healthy' && revisions == Array.new(count, revision)
      end
      check(Process.clock_gettime(Process::CLOCK_MONOTONIC) < deadline, 'ArgoCD 未在期限内成为 Synced/Healthy 且与回滚提交一致')
      sleep 1
    end
  end

  def finish_rollback(args, attempt)
    env = args.fetch(:env)
    context = args.fetch(:context)
    revision = args.fetch(:sync)
    paths = repo_paths(env)
    head = capture('读取当前工作树 HEAD', 'git', 'rev-parse', 'HEAD').strip
    check(head == revision, '执行同步前必须检出经过审阅的回滚提交')
    check(capture('检查 Git 工作树', 'git', 'status', '--porcelain', '--untracked-files=no', '--', *paths).empty?, '工作树发布文件与回滚提交不同')
    subject = capture('读取回滚提交主题', 'git', 'log', '-1', '--format=%s', revision).strip
    check(subject == "delivery: rollback #{env}", '仅同步由该流程创建的对应环境回滚提交')
    version, components = release(file(paths.last), env)
    local_context(context) if env == 'kind'
    running_schema(context, version, components)
    verify_registry(components)
    charts = matching_charts(env, paths.first, components)
    local_gitops(context, revision, args.fetch(:root_revision)) if env == 'kind'
    targets = sync_targets(context, env, charts, revision)
    plan = { revision: revision, applications: targets.keys }
    plan[:root_revision] = args[:root_revision] if env == 'kind'
    atomic_write(File.join(attempt, 'sync-plan.json'), JSON.pretty_generate(plan) + "\n")
    targets.each do |name, count|
      revisions = if count == 1
                    ['--revision', revision]
                  else
                    (1..count).flat_map { |position| ['--revisions', revision, '--source-positions', position.to_s] }
                  end
      argocd(context, 'app', 'sync', name, '--async', '--timeout', args.fetch(:timeout).to_s, *revisions)
    end
    if env == 'kind'
      wait_local(args, attempt, args.fetch(:timeout))
    else
      wait_for_revision(context, env, targets, revision, args.fetch(:timeout))
    end
    check(system({ 'RELEASE_MANIFEST' => File.expand_path(paths.last) }, 'bash', 'scripts/delivery-smoke.sh', '--env', env,
                 '--context', context, '--profile', 'readiness', '--evidence-dir', attempt), '回滚后烟测命令失败')
    smoke(File.join(attempt, "smoke-#{env}.json"), env, components, 'readiness', context: context)
    env == 'kind' ? wait_local(args, attempt, 1) : wait_for_revision(context, env, targets, revision, 1)
    running_schema(context, version, components)
    now = Time.now.utc
    marker = {
      environment: env, context: context, action: 'rollback', revision: revision, digest: components.fetch(CORE).fetch('digest'),
      schema_version: version, smoke_passed: true, rolled_back_at: now.iso8601
    }
    path = "dist/delivery/rollback-#{env}-#{now.strftime('%Y%m%dT%H%M%SZ')}-#{revision[0, 12]}.json"
    atomic_write(path, JSON.pretty_generate(marker) + "\n")
    puts "回滚验证通过：#{revision}；证据 #{path}"
  end

  def main(mode)
    check(%w[promote rollback].include?(mode), '未知交付操作')
    args = options(mode)
    env = args.fetch(:env)
    if mode == 'rollback'
      Dir.mkdir('dist') unless File.directory?('dist')
      FileUtils.mkdir_p('dist/delivery/rollback-attempts')
      attempt = Dir.mktmpdir("#{env}-", 'dist/delivery/rollback-attempts')
    end
    return finish_rollback(args, attempt) if args[:sync]

    version, components = inputs(args, mode)
    if mode == 'rollback' && args[:commit]
      local_context(args.fetch(:context)) if env == 'kind'
      running_schema(args.fetch(:context), version, components)
    end
    verify_registry(components, args[:proof])
    release_path = repo_paths(env).last
    if File.file?(release_path)
      current = yaml(file(release_path), '当前发布清单')
      entries = current['components']
      if entries.is_a?(Array) && current['schema_version'].to_s == version.to_s
        check(entries.all? { |entry| entry.is_a?(Hash) && entry['component'].is_a?(String) }, '当前发布组件格式无效')
        current_images = entries.to_h { |entry| [entry.fetch('component'), entry.slice('component', 'repository', 'digest', 'commit', 'chartVersion')] }
        check(current_images != components, '目标环境已经钉住同一发布，不生成空操作提交')
      end
    end
    candidate = versions(env, version, components)
    unless args[:commit]
      Tempfile.create(['.delivery-values-', '.yaml']) do |temporary|
        temporary.write(candidate.values.first)
        temporary.flush
        validate_render(env, temporary.path, components)
      end
      puts '发布计划验证通过；没有修改发布文件、生成提交、推送或同步。需要本地提交时显式传入 --commit。'
      return
    end

    revision = commit_release(mode, env, candidate, components)
    puts "已生成本地 #{env} 发布提交 #{revision}；未推送、未同步、未执行集群验收。"
    if mode == 'rollback'
      atomic_write(File.join(attempt, 'local-commit.json'), JSON.pretty_generate({ environment: env, revision: revision, schema_version: version }) + "\n")
      puts '请人工审阅并发布该提交，随后使用 --sync <完整提交 SHA> 执行同步与烟测。'
    elsif env == 'prod-like'
      puts 'prod-like 不会自动同步；请进入人工变更控制流程。'
    end
  rescue Failure, OptionParser::ParseError, SystemCallError => e
    if attempt
      # Keep sanitized diagnostics separate from successful rollback evidence.
      atomic_write(File.join(attempt, 'failure.json'), JSON.pretty_generate({ environment: env, error: e.message }) + "\n")
      warn "回滚未通过；失败证据：#{attempt}"
    end
    warn "错误：#{e.message}"
    exit 1
  end
end

DeliveryReleaseFlow.main(ARGV.shift) if $PROGRAM_NAME == __FILE__
