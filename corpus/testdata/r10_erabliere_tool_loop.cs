// From ErabliereApi/ErabliereApi, ErabliereApi/Services/AI/ConversationAIService.cs,
// with the Anthropic provider from Services/AI/AnthropicAIService.cs, the French
// system prompt from Services/AI/SystemPromptBuilder.cs and the loop bounds from
// Services/AI/Tools/ErabliereAiToolOptions.cs, which is where the repository keeps
// them. The model is read off configuration with DefaultModel as the fallback; the
// Azure OpenAI and Gemini providers behind the same IAIService are dropped so one
// model string is left, and the single prompt path without history goes with
// them. The prompts are French in the original and are kept that way, with the em
// dashes flattened.

using Anthropic;
using Anthropic.Exceptions;
using Anthropic.Models.Messages;
using ErabliereApi.Depot.Sql;
using ErabliereApi.Donnees;
using ErabliereApi.Donnees.Action.Post;
using ErabliereApi.Donnees.Contantes;
using ErabliereApi.Services.AI.Tools;
using Microsoft.EntityFrameworkCore;
using Microsoft.Extensions.Options;
using OpenAI.Chat;
using System.ClientModel;
using System.Globalization;
using System.Text;

namespace ErabliereApi.Services.AI;

/// <summary>
/// Service pour interagir avec l'api de messages d'Anthropic (Claude)
/// </summary>
public class AnthropicAIService : IAIService
{
    private readonly IConfiguration _config;

    /// <summary>
    /// Le nombre maximal de jetons de sortie quand <c>AnthropicMaxTokens</c> n'est
    /// pas configuré. L'api d'Anthropic exige ce paramètre, contrairement aux deux
    /// autres fournisseurs, et les jetons de réflexion du modèle comptent dedans :
    /// une valeur plus basse tronquerait la réponse au milieu de sa pensée.
    /// </summary>
    public const int DefaultMaxTokens = 16000;

    /// <summary>
    /// Le modèle utilisé quand <c>AnthropicModel</c> n'est pas configuré.
    /// </summary>
    public const string DefaultModel = "claude-opus-5";

    public AnthropicAIService(IConfiguration config)
    {
        _config = config;
    }

    /// <remarks>
    /// Set <c>AnthropicEnableToolCalling</c> to "false" to take the tools away from
    /// this provider without changing the rest of the configuration; the chat then
    /// answers from the model's own knowledge, as it did before tool calling existed.
    /// </remarks>
    public bool SupportsToolCalling =>
        !string.Equals(_config["AnthropicEnableToolCalling"], "false", StringComparison.OrdinalIgnoreCase);

    /// <remarks>
    /// The temperature and penalty knobs of <paramref name="chatCompletion" /> are
    /// deliberately not forwarded: Anthropic has no frequency or presence penalty,
    /// and the current Claude models reject a temperature with a 400. The lowered
    /// temperature the tool loop asks for during a tool exchange is therefore a
    /// no-op with this provider.
    /// </remarks>
    public async Task<AIResponse?> CompleteChatAsync(IEnumerable<ChatMessage> messages, ChatCompletionOptions chatCompletion, CancellationToken token)
    {
        var client = new AnthropicClient { ApiKey = _config["AnthropicApiKey"] };
        var system = AnthropicRequestMapper.MapSystem(messages);

        try
        {
            var response = await client.Messages.Create(new MessageCreateParams
            {
                Model = _config["AnthropicModel"] ?? DefaultModel,
                MaxTokens = ResolveMaxTokens(),
                System = system == null ? null : (MessageCreateParamsSystem)system,
                Messages = AnthropicRequestMapper.MapMessages(messages),
                Tools = AnthropicRequestMapper.MapTools(chatCompletion),
                Metadata = string.IsNullOrEmpty(chatCompletion.EndUserId)
                    ? null
                    : new Metadata { UserID = chatCompletion.EndUserId }
            }, cancellationToken: token);

            return new AIResponse
            {
                Text = AnthropicRequestMapper.MapText(response),
                FinishReason = response.StopReason?.ToString(),
                Refusal = AnthropicRequestMapper.MapRefusal(response),
                ToolCalls = AnthropicRequestMapper.MapToolCalls(response)
            };
        }
        catch (AnthropicApiException e)
        {
            throw new ClientResultException(e.Message, response: null, innerException: e);
        }
    }

    private int ResolveMaxTokens()
    {
        return int.TryParse(_config["AnthropicMaxTokens"], out var maxTokens) && maxTokens > 0
            ? maxTokens
            : DefaultMaxTokens;
    }
}

/// <summary>
/// The bounds of the tool calling loop, bound from the ErabliereAI:Tools section.
/// </summary>
public class ErabliereAiToolOptions
{
    /// <summary>
    /// Master switch. Turning it off brings the chat back to the phase 7 behaviour:
    /// no tool definition is sent and no tool is ever executed.
    /// </summary>
    public bool Enabled { get; set; } = true;

    /// <remarks>
    /// The token budget below is the bound that actually protects the context; this
    /// one only stops a loop that goes nowhere.
    /// </remarks>
    public int MaxRounds { get; set; } = 8;

    /// <remarks>
    /// The default of the platform is 1, which is a reasonable setting for a
    /// conversation and a poor one for a tool loop, so the loop lowers it for itself
    /// instead of forcing a single value on both.
    /// </remarks>
    public float? Temperature { get; set; } = 0.2f;

    /// <summary>
    /// Ceiling on the estimated number of tokens all the tool results of one prompt
    /// may add up to. Once crossed, no further tool is called.
    /// </summary>
    public int TokenBudget { get; set; } = 12000;
}

/// <summary>
/// Default <see cref="ISystemPromptBuilder" /> implementation.
/// </summary>
/// <remarks>
/// The prompt is built in three layers, and the split matters because only the
/// first one is persisted: <see cref="DefaultSystemPrompt" /> is written on the
/// conversation when it is created, <see cref="AnswerInstructions" /> is rebuilt on
/// every completion, and <see cref="ToolsInstructions" /> is added only when the
/// tools are actually on the table.
/// </remarks>
public class SystemPromptBuilder : ISystemPromptBuilder
{
    /// <summary>
    /// How to answer, tools or no tools.
    /// </summary>
    public const string AnswerInstructions =
        "## Comment vous répondez\n\n" +
        "Répondez en français, dans la langue du métier : entaille, coulée, tubulure, bassin, vacuum, brix. " +
        "Allez au fait - la plupart des questions se règlent en quelques phrases ou un court tableau, " +
        "et un exploitant qui consulte son érablière entre deux tournées n'a pas le temps d'un rapport. " +
        "Donnez toujours un chiffre avec son unité et le moment auquel il se rapporte. " +
        "Interprétez au lieu de réciter : dites ce que la valeur signifie pour l'exploitation et ce qu'elle appelle comme geste, " +
        "plutôt que de répéter une statistique que l'utilisateur voit déjà dans ses tableaux de bord. " +
        "Ne posez une question de clarification que si vous ne pouvez vraiment pas avancer sans la réponse; " +
        "autrement, prenez l'hypothèse la plus probable, dites laquelle vous avez prise, et répondez.";

    /// <summary>
    /// Told to the model when the read-only tools are on the table.
    /// </summary>
    /// <remarks>
    /// Every tool already carries a description saying what it returns and when to
    /// reach for it, so none of that is repeated here. What a tool cannot say about
    /// itself is the method: how to find something whose name you do not know, what
    /// an empty result means, and that a turn may carry several calls. Those are the
    /// three ways a model quietly gives up on a question it could have answered.
    /// </remarks>
    public const string ToolsInstructions =
        "## Vos outils\n\n" +
        "Vous disposez d'outils de consultation qui lisent les données réelles de l'utilisateur : " +
        "érablières, capteurs et leurs relevés, alertes, notes, barils, dompeux, horaire et rapports. " +
        "Ils sont en lecture seule - vous ne pouvez rien modifier ni supprimer - et ils n'atteignent que " +
        "les données de la personne qui vous parle. Chaque outil répond une enveloppe portant trois champs : " +
        "summary, une phrase déjà rédigée; data, les valeurs; et truncated.\n\n" +
        "## Comment enquêter\n\n" +
        "Dès qu'une question porte sur ce qui se passe ou s'est passé à l'érablière, allez lire. " +
        "N'annoncez pas que vous allez chercher et ne demandez pas la permission : cherchez, puis répondez.\n\n" +
        "Ne filtrez pas une liste avec les mots de la question. Les paramètres de recherche par nom sont de " +
        "simples sous-chaînes sensibles à la casse, et les noms que l'exploitant a donnés à ses capteurs, à ses " +
        "barils et à ses dompeux sont du texte libre - « Brix », « Vac. principale », « T° cabane » - qui " +
        "ressemble rarement au vocabulaire de sa question. Listez tout, puis reconnaissez vous-même l'élément " +
        "qu'il vise. Une liste vide après un filtre veut dire que le filtre était mauvais, jamais que la donnée " +
        "n'existe pas : relancez sans filtre avant d'en conclure quoi que ce soit.\n\n" +
        "Groupez vos appels. Vous n'avez que quelques tours avant de devoir répondre, et un même tour peut " +
        "porter plusieurs appels indépendants. Demandez les trois capteurs d'un coup, ou les alertes et les " +
        "notes ensemble, plutôt qu'un appel par tour.\n\n" +
        "N'abandonnez pas au premier échec. Un outil qui échoue ou qui revient vide vous dit quoi corriger : " +
        "reprenez l'identifiant, retirez le filtre, élargissez la plage, puis réessayez. Ne dites que vous " +
        "n'avez pas accès à une donnée qu'après avoir réellement essayé de la lire.\n\n" +
        "Les identifiants s'obtiennent en cascade et ne se devinent jamais : list_erablieres donne l'érablière, " +
        "list_capteurs donne ses capteurs, et get_donnees_capteur lit enfin les relevés.\n\n" +
        "Quand truncated vaut true, la fenêtre demandée contenait plus de relevés qu'il n'était possible d'en " +
        "lire d'un coup : resserrez-la et relisez avant de tirer une conclusion.\n\n" +
        "## Les dates\n\n" +
        "Nous sommes aujourd'hui le {0}. get_donnees_capteur exige une plage de dates, alors traduisez " +
        "vous-même ce que dit l'utilisateur : « en ce moment » ou « aujourd'hui », les dernières 24 heures; " +
        "« hier », la journée d'hier; « cette semaine », les 7 derniers jours; « la saison », depuis le " +
        "1er février. Une question posée sans date porte presque toujours sur les jours qui viennent de " +
        "passer. Au Québec, la coulée s'étend de la fin février à la mi-avril.\n\n" +
        "## Ce que vous affirmez\n\n" +
        "Ne citez que des valeurs que vous avez réellement lues, avec leur unité et leur date. " +
        "N'inventez jamais un chiffre. Si une lecture a échoué, dites simplement laquelle et pourquoi.";

    /// <inheritdoc />
    public string DefaultSystemPrompt =>
        "Vous êtes ÉrablièreAI, l'assistant de la plateforme ErabliereApi. " +
        "Vous êtes un acériculteur d'expérience doublé d'un analyste de données : vous connaissez autant la " +
        "science de l'érable - coulée, cycles de gel et de dégel, vacuum, concentration en sucre, osmose " +
        "inverse, évaporation, classification du sirop - que la conduite quotidienne d'une érablière " +
        "instrumentée. Vous parlez à l'exploitant, de son érablière à lui.";

    /// <inheritdoc />
    public string BuildForCompletion(string? conversationSystemMessage, ErabliereAiPromptContext context)
    {
        var prompt = new StringBuilder(string.IsNullOrWhiteSpace(conversationSystemMessage) ?
            DefaultSystemPrompt :
            conversationSystemMessage);

        prompt.Append("\n\n");
        prompt.Append(AnswerInstructions);

        if (context.ToolsEnabled)
        {
            prompt.Append("\n\n");
            prompt.AppendFormat(
                CultureInfo.InvariantCulture,
                ToolsInstructions,
                DateTimeOffset.Now.ToString("yyyy-MM-dd", CultureInfo.InvariantCulture));
        }

        AppendErabliereContext(prompt, context);
        AppendCapteurs(prompt, context);

        return prompt.ToString();
    }

    /// <summary>
    /// Names the maple grove the chat was opened from, so the model does not have to
    /// guess which one "mon érablière" means, nor spend a tool call finding out.
    /// </summary>
    private static void AppendErabliereContext(StringBuilder prompt, ErabliereAiPromptContext context)
    {
        if (context.ErabliereId is null || context.ErabliereId == Guid.Empty)
        {
            return;
        }

        prompt.Append("\n\n## L'érablière consultée\n\n");

        if (string.IsNullOrWhiteSpace(context.ErabliereNom))
        {
            prompt.AppendFormat(
                CultureInfo.InvariantCulture,
                "L'utilisateur consulte présentement l'érablière dont l'identifiant est {0}. Utilisez cet identifiant lorsqu'il parle de « mon érablière » sans en nommer une autre.",
                context.ErabliereId);
        }
        else
        {
            prompt.AppendFormat(
                CultureInfo.InvariantCulture,
                "L'utilisateur consulte présentement l'érablière « {0} », dont l'identifiant est {1}. Utilisez cet identifiant lorsqu'il parle de « mon érablière » sans en nommer une autre.",
                context.ErabliereNom.Trim(),
                context.ErabliereId);
        }

        prompt.Append(" Cet identifiant vous épargne un appel à list_erablieres.");
    }

    /// <summary>
    /// Names the sensors of the maple grove, with their identifiers, so the model
    /// reads the real names, picks one, and calls get_donnees_capteur with an
    /// identifier it did not invent.
    /// </summary>
    private static void AppendCapteurs(StringBuilder prompt, ErabliereAiPromptContext context)
    {
        if (context.Capteurs is not { Count: > 0 } capteurs)
        {
            return;
        }

        prompt.Append("\n\n## Les capteurs de cette érablière\n\n");
        prompt.Append("Voici la liste complète, déjà lue pour vous : n'appelez pas list_capteurs pour la retrouver. ");
        prompt.Append("Reconnaissez vous-même celui que vise la question, même si l'utilisateur le désigne avec " +
                      "d'autres mots que son nom, et lisez-le avec l'identifiant donné ici. ");
        prompt.Append("Si aucun ne correspond, dites-le : c'est que ce capteur n'existe pas.\n");

        foreach (var capteur in capteurs)
        {
            prompt.AppendFormat(
                CultureInfo.InvariantCulture,
                "\n- « {0} »{1}{2} - identifiant {3}",
                capteur.Nom,
                capteur.Symbole is { } symbole ? $" ({symbole})" : "",
                capteur.Type is { } type ? $", {type}" : "",
                capteur.Id);
        }
    }
}

/// <summary>
/// Default <see cref="IConversationAIService" /> implementation.
/// </summary>
public class ConversationAIService : IConversationAIService
{
    /// <summary>
    /// Told to the model on the turn that follows a limit, when the tools are taken
    /// away. Without it, a model that was mid-investigation tends to answer that it
    /// needs one more call, which the user can do nothing about.
    /// </summary>
    public const string LimitReachedInstruction =
        "Vous ne pouvez plus consulter d'outil pour cette question. " +
        "Répondez maintenant avec les données déjà obtenues, en précisant clairement ce que vous n'avez pas pu vérifier.";

    private readonly ErabliereDbContext _depot;
    private readonly IConfiguration _configuration;
    private readonly IAIService _aiService;
    private readonly ISystemPromptBuilder _systemPromptBuilder;
    private readonly IErabliereAiToolset _toolset;
    private readonly IErabliereAiCapteurCatalog _capteurCatalog;
    private readonly IErabliereAiCapabilityService _capabilityService;
    private readonly IToolActivityTracker _activityTracker;
    private readonly IOptions<ErabliereAiToolOptions> _toolOptions;
    private readonly ILogger<ConversationAIService> _logger;

    public ConversationAIService(
        ErabliereDbContext depot,
        IConfiguration configuration,
        IAIService aiService,
        ISystemPromptBuilder systemPromptBuilder,
        IErabliereAiToolset toolset,
        IErabliereAiCapteurCatalog capteurCatalog,
        IErabliereAiCapabilityService capabilityService,
        IToolActivityTracker activityTracker,
        IOptions<ErabliereAiToolOptions> toolOptions,
        ILogger<ConversationAIService> logger)
    {
        _depot = depot;
        _configuration = configuration;
        _aiService = aiService;
        _systemPromptBuilder = systemPromptBuilder;
        _toolset = toolset;
        _capteurCatalog = capteurCatalog;
        _capabilityService = capabilityService;
        _activityTracker = activityTracker;
        _toolOptions = toolOptions;
        _logger = logger;
    }

    /// <summary>
    /// Complete a prompt using the whole history of the conversation, calling the
    /// read-only tools when the caller's plan opens them.
    /// </summary>
    private async Task<AiExchange> CompleteChatAsync(PostPrompt prompt, Conversation conversation, CancellationToken token)
    {
        var tools = await ResolveToolsAsync(token);

        // Read as the caller, before the model is asked anything: it is one call, it
        // saves the round the model would have spent listing them, and it removes the
        // guess that a sensor whose name shares no word with the question is missing.
        var capteurs = tools.Count > 0 && prompt.ErabliereId is { } erabliereId ?
            await _capteurCatalog.ReadAsync(erabliereId, token) :
            [];

        var messagesPrompt = new List<ChatMessage>
        {
            new SystemChatMessage(_systemPromptBuilder.BuildForCompletion(
                conversation.SystemMessage,
                new ErabliereAiPromptContext(tools.Count > 0, prompt.ErabliereId, prompt.ErabliereNom, capteurs)))
        };

        var messages = await _depot.Messages
            .Where(m => m.ConversationId == prompt.ConversationId)
            .OrderBy(m => m.CreatedAt)
            .ToListAsync(token);

        foreach (var message in messages)
        {
            // The tool calls and results of the previous prompts are persisted for
            // the user interface, not replayed here: the answer that followed them
            // already carries what they said, and an assistant message whose tool
            // calls are not immediately followed by their results is rejected by the
            // chat completion apis.
            if (TypesMessage.EstMessageOutil(message.MessageType))
            {
                continue;
            }

            messagesPrompt.Add(message.IsUser ?
                new UserChatMessage(message.Content) :
                new AssistantChatMessage(message.Content));
        }

        messagesPrompt.Add(BuildUserMessage(prompt));

        try
        {
            if (tools.Count == 0)
            {
                var answer = await _aiService.CompleteChatAsync(messagesPrompt, BuildCompletionOptions(conversation), token);

                return new AiExchange(answer, []);
            }

            return await RunToolLoopAsync(prompt, conversation, messagesPrompt, tools, token);
        }
        catch (ClientResultException e)
        {
            throw new AIChatCompletionException(e);
        }
    }

    /// <summary>
    /// The tool calling loop: ask the model, run what it asked for, hand the results
    /// back, and start over until it answers or a limit is reached.
    /// </summary>
    /// <remarks>
    /// Three bounds close the loop, and all three end the same way - one last
    /// completion with the tools removed - so the user always gets the best answer
    /// that could be built rather than an error: at most <c>MaxRounds</c> turns in
    /// which tools are offered, a per tool timeout enforced by the tool set itself,
    /// and a budget on what the tool results are allowed to cost in context.
    /// </remarks>
    private async Task<AiExchange> RunToolLoopAsync(
        PostPrompt prompt,
        Conversation conversation,
        List<ChatMessage> messagesPrompt,
        IReadOnlyList<ChatTool> tools,
        CancellationToken token)
    {
        var options = _toolOptions.Value;
        var results = new List<ErabliereAiToolResult>();
        var spentTokens = 0;

        for (var round = 1; round <= options.MaxRounds; round++)
        {
            _activityTracker.Publish(prompt.ActivityId, new ToolActivityStep(round, null, ToolActivityLabels.Thinking));

            var response = await _aiService.CompleteChatAsync(
                messagesPrompt,
                BuildCompletionOptions(conversation, tools, toolExchange: true),
                token);

            if (response is null || response.ToolCalls.Count == 0)
            {
                return new AiExchange(response, results);
            }

            messagesPrompt.Add(BuildAssistantToolCallMessage(response.ToolCalls));

            foreach (var toolCall in response.ToolCalls)
            {
                _activityTracker.Publish(
                    prompt.ActivityId,
                    new ToolActivityStep(round, toolCall.FunctionName, ToolActivityLabels.For(toolCall.FunctionName)));

                var result = await _toolset.InvokeAsync(toolCall, token);

                results.Add(result);
                spentTokens += result.EstimatedTokens;

                messagesPrompt.Add(new ToolChatMessage(toolCall.Id, result.ResultJson));
            }

            if (spentTokens >= options.TokenBudget)
            {
                _logger.LogInformation(
                    "ErabliereAI stopped calling tools after {Rounds} rounds: the results reached the budget of {Budget} tokens.",
                    round, options.TokenBudget);

                break;
            }
        }

        return new AiExchange(await CompleteWithoutToolsAsync(conversation, messagesPrompt, token), results);
    }

    /// <summary>
    /// The turn that closes a loop stopped by a limit. The tools are not declared, so
    /// the model has no choice but to answer with what it gathered.
    /// </summary>
    private async Task<AIResponse?> CompleteWithoutToolsAsync(
        Conversation conversation,
        List<ChatMessage> messagesPrompt,
        CancellationToken token)
    {
        messagesPrompt.Add(new SystemChatMessage(LimitReachedInstruction));

        // Still the temperature of a tool exchange: this is the turn that writes the
        // numbers that were read into a sentence, which is exactly where an invented
        // one would do the most damage.
        return await _aiService.CompleteChatAsync(
            messagesPrompt,
            BuildCompletionOptions(conversation, toolExchange: true),
            token);
    }

    /// <summary>
    /// The tools this prompt may use: none unless the feature is on, the provider can
    /// be driven through a tool loop, and the caller's plan grants the capability.
    /// </summary>
    private async Task<IReadOnlyList<ChatTool>> ResolveToolsAsync(CancellationToken token)
    {
        if (!_aiService.SupportsToolCalling)
        {
            return [];
        }

        var capabilities = await _capabilityService.GetCapabilitiesAsync(token);

        return capabilities.ToolsEnabled ? _toolset.GetChatTools() : [];
    }

    private static AssistantChatMessage BuildAssistantToolCallMessage(IReadOnlyList<AIToolCall> toolCalls)
    {
        return new AssistantChatMessage(toolCalls.Select(toolCall => ChatToolCall.CreateFunctionToolCall(
            toolCall.Id,
            toolCall.FunctionName,
            BinaryData.FromString(string.IsNullOrWhiteSpace(toolCall.FunctionArguments) ? "{}" : toolCall.FunctionArguments))));
    }

    /// <summary>
    /// The options of one completion.
    /// </summary>
    /// <param name="toolExchange">
    /// True on every completion of a tool driven exchange, the rounds that declare
    /// tools and the one that closes them alike. It lowers the temperature, because
    /// reading data and writing what was read both want the model to stick to what is
    /// in front of it rather than to be inventive.
    /// </param>
    private ChatCompletionOptions BuildCompletionOptions(
        Conversation conversation,
        IReadOnlyList<ChatTool>? tools = null,
        bool toolExchange = false)
    {
        var options = new ChatCompletionOptions
        {
            Temperature = ResolveTemperature(toolExchange),
            FrequencyPenalty = 0,
            PresencePenalty = 0,
            EndUserId = MD5Hash(conversation.UserId)
        };

        if (tools != null)
        {
            foreach (var tool in tools)
            {
                options.Tools.Add(tool);
            }
        }

        return options;
    }

    /// <summary>
    /// The temperature of a completion: the one of the tool options during a tool
    /// exchange, the one of the platform otherwise.
    /// </summary>
    private float ResolveTemperature(bool toolExchange)
    {
        var temperature = _configuration.GetRequiredValue<float>("LLMDefaultTemperature");

        return toolExchange ? _toolOptions.Value.Temperature ?? temperature : temperature;
    }
}
