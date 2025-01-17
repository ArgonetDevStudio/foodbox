package shanepark.foodbox.slack.service;

import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Service;
import shanepark.foodbox.api.domain.MenuResponse;
import shanepark.foodbox.api.service.MenuService;
import shanepark.foodbox.slack.SlackConfig;
import shanepark.foodbox.slack.domain.SlackPayload;

import java.io.IOException;
import java.time.DayOfWeek;
import java.time.LocalDateTime;
import java.util.stream.Collectors;

@Service
@RequiredArgsConstructor
@Slf4j
public class SlackNotifyService {

    private final MenuService menuService;
    private final SlackMessageSender slackMessageSender;
    private final SlackConfig slackConfig;

    @Scheduled(cron = "0 0 9 * * *")
    public void notifyTodayMenu() throws IOException, InterruptedException {
        if (LocalDateTime.now().getDayOfWeek().getValue() > 5) {
            log.info("Today is weekend. Skip notifying today's menu.");
            return;
        }

        MenuResponse todayMenu = menuService.getTodayMenu();
        if (!todayMenu.isValid()) {
            log.info("Invalid menu. Skip notifying today's menu: {}", todayMenu);
            return;
        }

        String message = createSlackMessage(todayMenu);
        SlackPayload payload = new SlackPayload(slackConfig.getSlackChannel(), slackConfig.getUserName(), message, ":bento:");

        slackMessageSender.sendMessage(slackConfig.getSlackUrl(), slackConfig.getSlackToken(), payload);
    }

    private String createSlackMessage(MenuResponse todayMenu) {
        String menus = todayMenu.menus().stream()
                .map(menu -> "• " + menu)
                .collect(Collectors.joining("\n"));
        DayOfWeek dayOfWeek = todayMenu.date().getDayOfWeek();
        String dayOfWeekKor = getDayOfWeekKor(dayOfWeek);

        return String.format("<%s %s> 점심 메뉴\n%s", todayMenu.date(), dayOfWeekKor, menus);
    }

    private String getDayOfWeekKor(DayOfWeek dayOfWeek) {
        return switch (dayOfWeek) {
            case MONDAY -> "월요일";
            case TUESDAY -> "화요일";
            case WEDNESDAY -> "수요일";
            case THURSDAY -> "목요일";
            case FRIDAY -> "금요일";
            case SATURDAY -> "토요일";
            case SUNDAY -> "일요일";
        };
    }

}
