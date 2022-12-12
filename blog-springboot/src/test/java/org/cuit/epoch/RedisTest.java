package org.cuit.epoch;

import org.cuit.epoch.service.RedisService;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;

import java.util.HashSet;
import java.util.Set;

import static org.cuit.epoch.constant.RedisPrefixConst.USER_ONLINE;

/**
 * @author: ladidol
 * @date: 2022/12/2 12:28
 * @description:
 */
@SpringBootTest
public class RedisTest {
    @Autowired
    RedisService redisService;

    @Test
    void contextLoads() {

        redisService.set(USER_ONLINE, new HashSet<Integer>());
        Set<Integer> onlineUsers = (Set<Integer>) redisService.get(USER_ONLINE);

        onlineUsers.add(1);
        onlineUsers.add(222);
        onlineUsers.add(3);
        onlineUsers.add(4);
        onlineUsers.remove(222);

        redisService.set(USER_ONLINE, onlineUsers);

        System.out.println("有点怪");

    }


}