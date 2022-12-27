package org.cuit.epoch.config;

import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.web.socket.server.standard.ServerEndpointExporter;

/**
 * @author: Xiaoqiang-Ladidol
 * @date: 2022/12/26 16:55
 * @description: {websocket配置类}
 */
@Configuration
public class WebSocketConfig {

    /**
     *
     * 注入一个ServerEndpointExporter，该Bean会自动注册使用@ServerEndpoint注解申明的websocket endpoint
     * @return
     */
    @Bean
    public ServerEndpointExporter serverEndpointExporter() {
        return new ServerEndpointExporter();
    }

}
