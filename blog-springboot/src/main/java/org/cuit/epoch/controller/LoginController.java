package org.cuit.epoch.controller;

import cn.dev33.satoken.stp.StpUtil;
import cn.dev33.satoken.util.SaResult;
import cn.hutool.core.bean.BeanUtil;
import com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper;
import com.baomidou.mybatisplus.core.toolkit.StringUtils;
import eu.bitwalker.useragentutils.UserAgent;
import io.swagger.annotations.Api;
import io.swagger.annotations.ApiOperation;
import lombok.extern.slf4j.Slf4j;
import org.cuit.epoch.dto.UserDetailDTO;
import org.cuit.epoch.entity.UserAuth;
import org.cuit.epoch.entity.UserInfo;
import org.cuit.epoch.exception.AppException;
import org.cuit.epoch.mapper.RoleMapper;
import org.cuit.epoch.mapper.UserAuthMapper;
import org.cuit.epoch.mapper.UserInfoMapper;
import org.cuit.epoch.service.RedisService;
import org.cuit.epoch.service.UserAuthService;
import org.cuit.epoch.util.IpUtils;
import org.cuit.epoch.util.PasswordUtils;
import org.cuit.epoch.util.Result;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

import javax.annotation.PostConstruct;
import javax.annotation.Resource;
import javax.servlet.http.HttpServletRequest;
import java.time.LocalDateTime;
import java.time.ZoneId;
import java.util.List;
import java.util.Objects;
import java.util.Set;

import static org.cuit.epoch.enums.RedisPrefixConst.*;
import static org.cuit.epoch.enums.ZoneEnum.SHANGHAI;

/**
 * @author: ladidol
 * @date: 2022/11/16 15:35
 * @description:
 */
@RestController
@Slf4j
@Api(tags = "登录模块")
public class LoginController {

    @Autowired
    UserAuthService userAuthService;

    @Resource
    private HttpServletRequest request;


    @PostMapping("login")
    @ApiOperation(value = "账号密码登录")
    public Result<UserDetailDTO> login(String username, String password) {
        UserDetailDTO userDetailDTO = userAuthService.login(username, password);
        return Result.ok(userDetailDTO);
    }

    @GetMapping("logout")
    @ApiOperation(value = "注销登录")
    public Result<String> logout() {
        userAuthService.logout();
        return Result.ok("注销成功");
    }


}