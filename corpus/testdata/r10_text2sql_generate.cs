// From shuyu-labs/Text2Sql.Net, src/Text2Sql.Net/Domain/Service/ChatService.cs,
// with the prompt from Domain/Service/AdvancedPromptService.cs, the kernel wiring
// from Extensions/ServiceCollectionExtensions.cs and the option class from
// Common/Options/Text2SqlOpenAIOption.cs, which is where the repository keeps
// them. The chat model is bound from appsettings.json, whose sample value the
// catalog does not price, so the catalog id gpt-4.1 is set as the option's default
// here. The prompt is Chinese in the original and is kept that way; its built in
// example, reasoning chain and profile sections are trimmed to the role, schema
// and output blocks, and the safety check and feedback rounds that follow the
// generation are dropped.

using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Logging;
using Microsoft.SemanticKernel;
using Microsoft.SemanticKernel.Connectors.OpenAI;
using System.Text;
using Text2Sql.Net.Domain.Interface;
using Text2Sql.Net.Options;
using Text2Sql.Net.Repositories.Text2Sql.ChatHistory;
using Text2Sql.Net.Repositories.Text2Sql.DatabaseConnection;

namespace Text2Sql.Net.Options
{
    public class Text2SqlOpenAIOption
    {
        /// <summary>
        /// 聊天模型的端点地址
        /// </summary>
        public static string? EndPoint { get; set; }

        /// <summary>
        /// 聊天模型的API密钥
        /// </summary>
        public static string? Key { get; set; }

        /// <summary>
        /// 聊天模型名称
        /// </summary>
        public static string? ChatModel { get; set; } = "gpt-4.1";
    }
}

namespace Microsoft.Extensions.DependencyInjection
{
    /// <summary>
    /// 容器扩展
    /// </summary>
    public static class ServiceCollectionExtensions
    {
        /// <summary>
        /// 初始化SK
        /// </summary>
        /// <param name="services"></param>
        /// <param name="_kernel">可以提供自定义Kernel</param>
        static void InitSK(IServiceCollection services,Kernel _kernel = null)
        {
            var handler = new OpenAIHttpClientHandler();
            services.AddTransient<Kernel>((serviceProvider) =>
            {
                if (_kernel == null)
                {
                    _kernel = Kernel.CreateBuilder()
                    .AddOpenAIChatCompletion(
                      modelId: Text2SqlOpenAIOption.ChatModel,
                      apiKey: Text2SqlOpenAIOption.Key,
                      httpClient: new HttpClient(handler)
                         )
                    .Build();
                }
                //导入插件
                if (!_kernel.Plugins.Any(p => p.Name == "text2sql"))
                {
                    var pluginPatth = Path.Combine(RepoFiles.SamplePluginsPath(), "text2sql");
                    _kernel.ImportPluginFromPromptDirectory(pluginPatth);
                }
                return _kernel;
            });
        }
    }
}

namespace Text2Sql.Net.Domain.Service
{
    /// <summary>
    /// 高级Prompt工程服务
    /// 实现Few-shot Learning、链式思考和个性化Prompt生成
    /// </summary>
    [ServiceDescription(typeof(IAdvancedPromptService), ServiceLifetime.Scoped)]
    public class AdvancedPromptService : IAdvancedPromptService
    {
        private readonly ILogger<AdvancedPromptService> _logger;

        public AdvancedPromptService(ILogger<AdvancedPromptService> logger)
        {
            _logger = logger;
        }

        /// <summary>
        /// 创建包含问答示例的渐进式复杂度Prompt
        /// </summary>
        /// <param name="userMessage">用户查询</param>
        /// <param name="schemaInfo">Schema信息</param>
        /// <param name="dbType">数据库类型</param>
        /// <param name="examplesPrompt">格式化的问答示例</param>
        /// <returns>优化后的Prompt</returns>
        public async Task<string> CreateProgressivePromptWithExamplesAsync(
            string userMessage,
            string schemaInfo,
            string dbType,
            string examplesPrompt)
        {
            var prompt = BuildProgressivePromptWithExamples(userMessage, schemaInfo, dbType, examplesPrompt);

            _logger.LogInformation("生成了渐进式Prompt" +
                (!string.IsNullOrEmpty(examplesPrompt) ? "，并包含了用户问答示例" : ""));
            return prompt;
        }

        /// <summary>
        /// 构建包含用户问答示例的渐进式Prompt
        /// </summary>
        private static string BuildProgressivePromptWithExamples(
            string userMessage,
            string schemaInfo,
            string dbType,
            string examplesPrompt)
        {
            var prompt = new StringBuilder();

            // 1. 角色定义和任务说明
            prompt.AppendLine("# 专业SQL查询生成专家");
            prompt.AppendLine();
            prompt.AppendLine("您是一位资深的SQL查询生成专家，具备深厚的数据库理论基础和丰富的实战经验。");
            prompt.AppendLine("您的任务是将自然语言查询转换为高效、准确的SQL语句。");
            prompt.AppendLine($"当前时间:{DateTime.Now}");
            prompt.AppendLine();

            // 2. 用户提供的问答示例（最高优先级 - 移到最前面）
            if (!string.IsNullOrEmpty(examplesPrompt))
            {
                prompt.AppendLine("## 【重要】参考示例 - 务必遵循");
                prompt.AppendLine();
                prompt.AppendLine("**以下是与当前查询高度相关的实际问答示例，这些示例来自相同的数据库环境，具有极高的参考价值。**");
                prompt.AppendLine();
                prompt.AppendLine("**请特别注意：**");
                prompt.AppendLine("1. 这些示例展示了在当前数据库中解决类似问题的正确方法");
                prompt.AppendLine("2. 必须严格参考示例中的SQL编写风格、表关联方式和查询模式");
                prompt.AppendLine("3. 示例中使用的表名、字段名、JOIN方式都是针对当前数据库优化过的");
                prompt.AppendLine("4. 优先采用示例中展示的查询结构和技巧");
                prompt.AppendLine();
                prompt.AppendLine(examplesPrompt);
                prompt.AppendLine();
            }

            // 3. 数据库信息
            prompt.AppendLine("## 数据库环境");
            prompt.AppendLine($"- **数据库类型**: {dbType}");
            prompt.AppendLine("- **表结构信息**:");
            prompt.AppendLine("```json");
            prompt.AppendLine(schemaInfo);
            prompt.AppendLine("```");
            prompt.AppendLine();

            // 4. 当前查询分析
            prompt.AppendLine("## 当前查询分析");
            prompt.AppendLine();
            prompt.AppendLine($"**用户问题**: {userMessage}");
            prompt.AppendLine();

            // 5. 输出要求
            prompt.AppendLine("## 输出要求");
            prompt.AppendLine();
            prompt.AppendLine("请按照以下格式输出：");
            prompt.AppendLine();
            prompt.AppendLine("1. **分析过程** (可选，根据用户偏好):");
            prompt.AppendLine("   - 简要说明您的分析思路");
            prompt.AppendLine();
            prompt.AppendLine("2. **SQL查询**:");
            prompt.AppendLine("   - 生成完整、可执行的SQL语句");
            prompt.AppendLine("   - 确保语法正确，符合指定数据库类型");
            prompt.AppendLine("   - 不要包含任何格式标记（如```sql```）");
            prompt.AppendLine();

            // 6. 质量检查清单
            prompt.AppendLine("## 质量检查清单");
            prompt.AppendLine();
            prompt.AppendLine("生成SQL前请确认：");
            prompt.AppendLine("- [ ] 表名和列名拼写正确");
            prompt.AppendLine("- [ ] JOIN条件准确无误");
            prompt.AppendLine("- [ ] WHERE条件逻辑正确");
            prompt.AppendLine("- [ ] 聚合函数使用恰当");
            prompt.AppendLine("- [ ] 排序和分页符合需求");
            prompt.AppendLine("- [ ] 语法符合目标数据库类型");
            prompt.AppendLine();

            prompt.AppendLine("现在请分析上述查询并生成对应的SQL语句：");

            return prompt.ToString();
        }
    }

    /// <summary>
    /// 聊天服务实现
    /// </summary>
    [ServiceDescription(typeof(IChatService), ServiceLifetime.Scoped)]
    public class ChatService : IChatService
    {
        private readonly IDatabaseConnectionConfigRepository _connectionRepository;
        private readonly IIntelligentSchemaLinkingService _schemaLinkingService;
        private readonly IAdvancedPromptService _promptService;
        private readonly IQAExampleService _qaExampleService;
        private readonly Kernel _kernel;
        private readonly ILogger<ChatService> _logger;

        /// <summary>
        /// 构造函数
        /// </summary>
        public ChatService(
            IDatabaseConnectionConfigRepository connectionRepository,
            IIntelligentSchemaLinkingService schemaLinkingService,
            IAdvancedPromptService promptService,
            IQAExampleService qaExampleService,
            Kernel kernel,
            ILogger<ChatService> logger)
        {
            _connectionRepository = connectionRepository;
            _schemaLinkingService = schemaLinkingService;
            _promptService = promptService;
            _qaExampleService = qaExampleService;
            _kernel = kernel;
            _logger = logger;
        }

        /// <inheritdoc/>
        public async Task<ChatMessage> GenerateAndExecuteSqlAsync(string connectionId, string userMessage)
        {
            _logger.LogInformation($"开始处理用户查询：{userMessage}");

            // 2. 获取相关的问答示例
            var relevantExamples = await _qaExampleService.GetRelevantExamplesAsync(connectionId, userMessage, limit: 3, minRelevanceScore: 0.6);
            string examplesPrompt = string.Empty;
            if (relevantExamples.Count > 0)
            {
                examplesPrompt = _qaExampleService.FormatExamplesForPrompt(relevantExamples);
                _logger.LogInformation($"找到{relevantExamples.Count}个相关问答示例");
            }

            // 3. 智能Schema Linking：获取相关表结构
            var schemaLinkingResult = await _schemaLinkingService.GetRelevantSchemaAsync(connectionId, userMessage);
            if (!schemaLinkingResult.Success)
            {
                return CreateErrorResponse(connectionId, schemaLinkingResult.ErrorMessage ?? "无法获取相关的数据库表结构信息");
            }

            var connectionConfig = await _connectionRepository.GetByIdAsync(connectionId);

            // 4. 高级Prompt工程：生成优化的Prompt（包含问答示例）
            var optimizedPrompt = await _promptService.CreateProgressivePromptWithExamplesAsync(
                userMessage,
                schemaLinkingResult.SchemaJson,
                connectionConfig.DbType,
                examplesPrompt);

            // 5. 使用优化Prompt生成SQL
            string sqlQuery = await GenerateSqlWithAdvancedPromptAsync(optimizedPrompt);
            if (string.IsNullOrEmpty(sqlQuery))
            {
                return CreateErrorResponse(connectionId, "无法生成SQL查询语句");
            }

            _logger.LogInformation($"生成的SQL：{sqlQuery}");

            return new ChatMessage
            {
                Id = Guid.NewGuid().ToString(),
                ConnectionId = connectionId,
                IsUser = false,
                SqlQuery = sqlQuery,
                CreateTime = DateTime.Now
            };
        }

        /// <summary>
        /// 使用高级Prompt生成SQL查询
        /// </summary>
        /// <param name="optimizedPrompt">优化后的Prompt</param>
        /// <returns>生成的SQL查询</returns>
        private async Task<string> GenerateSqlWithAdvancedPromptAsync(string optimizedPrompt)
        {
            try
            {
                OpenAIPromptExecutionSettings settings = new()
                {
                    Temperature = 0.1,
                    MaxTokens = 2000
                };

                // 直接使用优化后的完整Prompt
                var result = await _kernel.InvokePromptAsync(optimizedPrompt, new KernelArguments(settings));

                // 提取生成的SQL
                string sql = result?.ToString()?.Trim() ?? string.Empty;

                // 清理SQL结果
                sql = CleanSqlResult(sql);

                return sql;
            }
            catch (Exception ex)
            {
                _logger.LogError(ex, $"使用高级Prompt生成SQL时出错：{ex.Message}");
                return string.Empty;
            }
        }
    }
}
